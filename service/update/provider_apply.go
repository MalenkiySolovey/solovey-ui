package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"gorm.io/gorm"
)

// This is the existing SQLite owner's admission, retained only while Update
// waits for an executable restart. It is not another transaction or journal.
type providerDatabaseMaintenance interface {
	Context(context.Context) context.Context
	Active() bool
	End()
}

type providerPreparation struct{ request ProviderTransitionRequest }

type providerMaintenancePreflighter interface {
	PreflightProvider(context.Context, model.UpdateOperation, release.Verified, []release.Artifact, CoreConfigurationSnapshot,
		func(string) (model.UpdateOperation, error)) (PreflightResult, error)
	AdmitProviderRequest(context.Context, model.UpdateOperation, ProviderTransitionRequest) error
}

// ApplyProviderTransition uses the normal Update operation and journal. The
// explicit maintenance request binds the preview, rather than changing a
// runtime setting. Network staging completes before database admission closes.
func (m *LifecycleManager) ApplyProviderTransition(ctx context.Context, request ProviderTransitionRequest) (model.UpdateOperation, error) {
	if m == nil || ctx == nil || request.Validate() != nil {
		return model.UpdateOperation{}, ErrCoreProviderMaintenance
	}
	if _, err := actions.Require(ctx, ProviderTransitionAction(request), request.Authorize); err != nil {
		return model.UpdateOperation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.coreProvider == nil || !coreprovider.SourceIdentityVerified(m.coreProvider()) || m.configuration == nil {
		return model.UpdateOperation{}, ErrCoreProviderUnverified
	}
	if m.coreProvider().Revision != request.ExpectedProviderRevision {
		return model.UpdateOperation{}, ErrReleaseChanged
	}
	if _, ok := m.provider.(providerMaintenancePreflighter); !ok {
		return model.UpdateOperation{}, ErrProviderUnavailable
	}
	operation, err := m.prepareLocked(ctx, PrepareRequest{Channel: request.Channel, ExpectedSequence: request.ExpectedSequence,
		ExpectedManifestDigest: request.ExpectedManifestDigest, IdempotencyKey: request.IdempotencyKey, Acknowledged: true},
		&providerPreparation{request: request})
	if err == nil && State(operation.State) == StatePrepared {
		err = m.provider.(providerMaintenancePreflighter).AdmitProviderRequest(m.maintenanceContext(ctx), operation, request)
		if err == nil {
			operation, err = m.activateLocked(m.maintenanceContext(ctx), RevisionRequest{OperationID: operation.OperationID, ExpectedRevision: operation.Revision}, true)
		}
	}
	if State(operation.State) == StateApplied || State(operation.State) == StateRolledBack || State(operation.State) == StateFailed || operation.OperationID == "" {
		m.endProviderMaintenance()
	}
	return operation, err
}

func semanticBytesDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (m *LifecycleManager) maintenanceContext(ctx context.Context) context.Context {
	if m.maintenance != nil && m.maintenance.Active() {
		return m.maintenance.Context(ctx)
	}
	return ctx
}

func (m *LifecycleManager) endProviderMaintenance() {
	if m.maintenance != nil {
		m.maintenance.End()
		m.maintenance = nil
	}
}

func (m *LifecycleManager) preflightProviderLocked(ctx context.Context, operation *model.UpdateOperation, verified release.Verified, artifacts []release.Artifact,
	request ProviderTransitionRequest) (context.Context, PreflightResult, error) {
	provider, ok := m.provider.(providerMaintenancePreflighter)
	if !ok || m.configuration == nil || m.beginMaintenance == nil || m.maintenance != nil && m.maintenance.Active() {
		return ctx, PreflightResult{}, ErrCoreProviderMaintenance
	}
	owner, err := m.beginMaintenance(ctx)
	if err != nil {
		return ctx, PreflightResult{}, err
	}
	m.maintenance = owner
	ctx = owner.Context(ctx)
	snapshot, err := m.configuration.CaptureProviderConfiguration(ctx)
	defer snapshot.Clear()
	if err != nil || snapshot.ConfigurationRevision != request.ExpectedConfigurationRevision ||
		len(snapshot.Configuration) == 0 || semanticBytesDigest(snapshot.Configuration) != snapshot.ConfigurationDigest ||
		snapshot.Capabilities.Validate(m.coreProvider(), m.profile()) != nil || snapshot.Capabilities.Revision() != snapshot.CapabilityRevision {
		return ctx, PreflightResult{}, ErrReleaseChanged
	}
	result, err := provider.PreflightProvider(ctx, *operation, verified, artifacts, snapshot, func(backup string) (model.UpdateOperation, error) {
		if !validDigest(backup) {
			return *operation, ErrRecoveryRequired
		}
		checkpoint := *operation
		checkpoint.BackupRef, checkpoint.RollbackAvailable = backup, true
		committed, err := m.advance(ctx, checkpoint, StatePreflighting, "provider_database_checkpoint_committed", "")
		if err == nil {
			*operation = committed
		}
		return committed, err
	})
	return ctx, result, err
}

func pendingDatabaseRollback(provider Provider) bool {
	if pending, ok := provider.(interface{ HasPendingDatabaseRollback() bool }); ok {
		return pending.HasPendingDatabaseRollback()
	}
	return true
}

// Reconciliation probes get fresh operation fences, while their deadline stays
// anchored to the original journal entry rather than the latest retry.
func (r Repository) phaseStartedAt(ctx context.Context, operation model.UpdateOperation, phase State) (int64, error) {
	db, err := r.db(ctx)
	if err != nil {
		return 0, err
	}
	var entry model.UpdateJournal
	err = db.Where("operation_id = ? AND state = ?", operation.OperationID, phase).Order("revision").First(&entry).Error
	if err != nil || entry.CreatedAt <= 0 {
		return 0, errors.Join(ErrRecoveryRequired, err)
	}
	return entry.CreatedAt, nil
}

// RecoverProviderMaintenance runs before mutable app workers and Core startup.
// Authority comes from the normal active Update row and authenticated broker
// observation, never a restored database row alone.
func (m *LifecycleManager) RecoverProviderMaintenance(ctx context.Context) error {
	if m == nil {
		return ErrProviderUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	operation, err := m.repo.active(ctx)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	recorded := false
	if operation.OperationID != "" {
		recorded, err = m.repo.providerCheckpointRecorded(ctx, operation)
		if err != nil {
			return err
		}
	}
	if !recorded {
		probe, ok := m.provider.(interface {
			pendingProviderMaintenance(context.Context) (bool, error)
		})
		if !ok {
			return nil
		}
		pending, probeErr := probe.pendingProviderMaintenance(ctx)
		if !pending {
			return probeErr
		}
		if err := m.holdProviderRecovery(ctx); err != nil {
			return err
		}
		return ErrRecoveryRequired
	}
	if err := m.holdProviderRecovery(ctx); err != nil {
		return err
	}
	provider, ok := m.provider.(interface {
		providerCheckpoint(context.Context, model.UpdateOperation) (bool, error)
	})
	if !ok {
		return ErrRecoveryRequired
	}
	if !validPersistedOperation(operation) || operation.RestoredUntrusted {
		return ErrRecoveryRequired
	}
	if recovery, ok := m.provider.(interface{ requireProviderMaintenance(model.UpdateOperation) }); ok {
		recovery.requireProviderMaintenance(operation)
	}
	present, err := provider.providerCheckpoint(m.maintenanceContext(ctx), operation)
	if err != nil {
		return err
	}
	if !present {
		return ErrRecoveryRequired
	}
	return nil
}

func (m *LifecycleManager) holdProviderRecovery(ctx context.Context) error {
	if m.configuration == nil || m.beginMaintenance == nil {
		return ErrRecoveryRequired
	}
	if m.maintenance == nil || !m.maintenance.Active() {
		var err error
		m.maintenance, err = m.beginMaintenance(ctx)
		if err != nil {
			return err
		}
	}
	return m.configuration.SetCoreMaintenance(m.maintenanceContext(ctx), "", true)
}

func (r Repository) providerCheckpointRecorded(ctx context.Context, operation model.UpdateOperation) (bool, error) {
	db, err := r.db(ctx)
	if err != nil {
		return false, err
	}
	var count int64
	err = db.Model(&model.UpdateJournal{}).Where("operation_id = ? AND event = ?", operation.OperationID, "provider_database_checkpoint_committed").Limit(1).Count(&count).Error
	return count != 0, err
}

// A bounded reconciliation tick belongs to Update. It also handles an old
// process surviving a failed restart; no second provider worker is introduced.
func (m *LifecycleManager) RunMaintenanceWatchdog(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Staging may hold the manager lock longer than this worker's
			// shutdown budget. A missed tick can wait for the next tick;
			// cancellation must never wait for another operation's lock.
			if !m.mu.TryLock() {
				continue
			}
			if m.maintenance != nil && m.maintenance.Active() {
				check, cancel := context.WithTimeout(ctx, 30*time.Second)
				_ = m.reconcileStartupLocked(check)
				cancel()
			}
			m.mu.Unlock()
		}
	}
}
