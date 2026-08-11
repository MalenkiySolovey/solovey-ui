package datalifecycle

import (
	"context"
	"errors"
	"io"
	"strings"

	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	operationcoordination "github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"gorm.io/gorm"
)

type RestoreRequest struct {
	ExpectedRehearsalRevision string
	IdempotencyKey            string
	Confirmation              string
	Acknowledged              bool
	Source                    io.ReadSeeker
}

func RestoreConfirmation(revision string) string {
	if len(revision) < 12 {
		return ""
	}
	return "RESTORE_DATABASE_" + strings.ToUpper(revision[:12])
}

func (m *Manager) ExecuteRestore(ctx context.Context, request RestoreRequest) (model.DataLifecycleOperation, dbbackup.RestoreExecutionResult, error) {
	result := dbbackup.RestoreExecutionResult{}
	if m == nil || ctx == nil || request.Source == nil || !validDigest(request.ExpectedRehearsalRevision) ||
		!safeID(request.IdempotencyKey, 96) || request.Confirmation != RestoreConfirmation(request.ExpectedRehearsalRevision) || !request.Acknowledged {
		return model.DataLifecycleOperation{}, result, errors.New("invalid restore request")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, err := m.byIdempotency(ctx, request.IdempotencyKey); err == nil {
		if existing.Kind == "RESTORE" && existing.ExpectedRevision == request.ExpectedRehearsalRevision {
			return existing, result, nil
		}
		return existing, result, ErrPreviewChanged
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DataLifecycleOperation{}, result, err
	}
	if !m.admitted("heavy_mutation") {
		return model.DataLifecycleOperation{}, result, ErrOperationConflict
	}
	if blocker := m.globalOperationBlocker(ctx); blocker != "" {
		return model.DataLifecycleOperation{}, result, ErrOperationConflict
	}
	rehearsal, err := dbbackup.Rehearse(ctx, request.Source)
	if err != nil || !rehearsal.Possible || rehearsal.Revision != request.ExpectedRehearsalRevision {
		return model.DataLifecycleOperation{}, result, ErrPreviewChanged
	}
	if _, err := request.Source.Seek(0, io.SeekStart); err != nil {
		return model.DataLifecycleOperation{}, result, err
	}
	now := m.Now().UTC().Unix()
	manifestDigest := ""
	if rehearsal.Manifest != nil {
		manifestDigest = rehearsal.Manifest.BackupID
	}
	operation := model.DataLifecycleOperation{
		OperationID:    "data-operation:" + semanticDigest(struct{ Key, Revision string }{request.IdempotencyKey, rehearsal.Revision})[:48],
		IdempotencyKey: request.IdempotencyKey, Kind: "RESTORE", State: "ADMITTED", OwnerID: "core",
		ManifestDigest: manifestDigest, ExpectedRevision: rehearsal.Revision, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := operationcoordination.SerializeAdmission(func() error {
		if blocker := m.globalOperationBlocker(ctx); blocker != "" {
			return ErrOperationConflict
		}
		return m.create(ctx, operation, "restore_admitted", "")
	}); err != nil {
		return operation, result, err
	}
	operation, err = m.advance(ctx, operation, "RESTORING", "restore_started", "")
	if err != nil {
		return operation, result, err
	}
	result, err = dbbackup.RestoreContextDetailed(ctx, request.Source)
	if err != nil {
		failed, recordErr := m.fail(ctx, operation, "restore_failed", err)
		if recordErr != nil {
			return failed, result, errors.Join(err, recordErr)
		}
		return failed, result, err
	}
	operation.BackupRef = result.RecoveryBackupRef
	operation.State, operation.ReasonCode, operation.Revision, operation.UpdatedAt = "APPLIED", "", operation.Revision+1, m.Now().UTC().Unix()
	if err := m.writeRestoredOperation(ctx, operation); err != nil {
		return m.rollbackUnacceptedRestore(ctx, operation, result, "restore_journal_unavailable", err)
	}
	cleanupPending, restartPending, err := dbbackup.CompletePendingRestore(ctx)
	result.RecoveryCleanupPending, result.RestartPending = cleanupPending, restartPending
	if err != nil {
		return m.rollbackUnacceptedRestore(ctx, operation, result, "restore_acceptance_unavailable", err)
	}
	return operation, result, nil
}

func (m *Manager) rollbackUnacceptedRestore(
	ctx context.Context,
	operation model.DataLifecycleOperation,
	result dbbackup.RestoreExecutionResult,
	reason string,
	cause error,
) (model.DataLifecycleOperation, dbbackup.RestoreExecutionResult, error) {
	if rollbackErr := dbbackup.AbortPendingRestore(); rollbackErr != nil {
		operation.State, operation.ReasonCode = "RECOVERY_REQUIRED", reason
		return operation, result, errors.Join(ErrRecoveryRequired, cause, rollbackErr)
	}
	// The exact fallback contains the original RESTORING row. Close it
	// explicitly as rolled back so startup and idempotent replay agree.
	operation.State, operation.ReasonCode, operation.Revision =
		"RESTORING", "", operation.Revision-1
	rolledBack, recordErr := m.advance(ctx, operation, "ROLLED_BACK", "restore_candidate_rejected", reason)
	if recordErr != nil {
		rolledBack.State, rolledBack.ReasonCode = "RECOVERY_REQUIRED", reason
		return rolledBack, result, errors.Join(ErrRecoveryRequired, cause, recordErr)
	}
	return rolledBack, result, cause
}

func (m *Manager) writeRestoredOperation(ctx context.Context, operation model.DataLifecycleOperation) error {
	db := m.database()
	if db == nil {
		return errors.New("restored database is unavailable")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conflicts []model.DataLifecycleOperation
		if err := tx.Where("operation_id = ? OR idempotency_key = ?", operation.OperationID, operation.IdempotencyKey).
			Limit(3).Find(&conflicts).Error; err != nil {
			return err
		}
		if len(conflicts) > 1 && conflicts[0].OperationID != conflicts[1].OperationID {
			return errors.New("restored database operation identity is ambiguous")
		}
		for _, conflict := range conflicts {
			if err := tx.Where("operation_id = ?", conflict.OperationID).Delete(&model.DataLifecycleJournal{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("operation_id = ? OR idempotency_key = ?", operation.OperationID, operation.IdempotencyKey).
			Delete(&model.DataLifecycleOperation{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		return tx.Create(&model.DataLifecycleJournal{OperationID: operation.OperationID, State: operation.State,
			Event: "restore_applied", Revision: operation.Revision, CreatedAt: operation.UpdatedAt}).Error
	})
}
