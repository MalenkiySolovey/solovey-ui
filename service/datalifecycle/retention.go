package datalifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const (
	maxDataLifecycleTerminalOperations    = 128
	dataLifecycleTerminalRetentionAge     = 30 * 24 * time.Hour
	maxDataLifecycleTerminalLogicalBytes  = int64(8 << 20)
	maxDataLifecycleTerminalArtifacts     = 8
	maxDataLifecycleTerminalArtifactBytes = int64(512 << 20)
)

type RetentionPolicyV1 struct {
	TerminalOperations    int   `json:"terminalOperations"`
	TerminalAgeSeconds    int64 `json:"terminalAgeSeconds"`
	TerminalLogicalBytes  int64 `json:"terminalLogicalBytes"`
	TerminalArtifacts     int   `json:"terminalArtifacts"`
	TerminalArtifactBytes int64 `json:"terminalArtifactBytes"`
}

type PruneResult struct {
	DeletedOperations int   `json:"deletedOperations"`
	DeletedJournals   int64 `json:"deletedJournals"`
	DeletedArtifacts  int   `json:"deletedArtifacts"`
	DeletedBytes      int64 `json:"deletedBytes"`
	PreservedFiles    int   `json:"preservedFiles"`
}

func HistoryRetentionPolicy() RetentionPolicyV1 {
	return RetentionPolicyV1{
		TerminalOperations:    maxDataLifecycleTerminalOperations,
		TerminalAgeSeconds:    int64(dataLifecycleTerminalRetentionAge / time.Second),
		TerminalLogicalBytes:  maxDataLifecycleTerminalLogicalBytes,
		TerminalArtifacts:     maxDataLifecycleTerminalArtifacts,
		TerminalArtifactBytes: maxDataLifecycleTerminalArtifactBytes,
	}
}

func (m *Manager) Prune(ctx context.Context) (PruneResult, error) {
	if m == nil || ctx == nil {
		return PruneResult{}, errors.New("data lifecycle retention is unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result, err := m.pruneLocked(ctx)
	if err == nil {
		err = m.reconcileRecoveryReferencesLocked(ctx)
	}
	return result, err
}

func (m *Manager) maintainLocked(ctx context.Context) error {
	if _, err := m.pruneLocked(ctx); err != nil {
		return err
	}
	return m.reconcileRecoveryReferencesLocked(ctx)
}

func (m *Manager) pruneLocked(ctx context.Context) (PruneResult, error) {
	result := PruneResult{}
	db := m.database()
	if db == nil {
		return result, errors.New("data lifecycle database unavailable")
	}
	var operations []model.DataLifecycleOperation
	if err := db.WithContext(ctx).Order("updated_at DESC, operation_id DESC").Find(&operations).Error; err != nil {
		return result, err
	}
	cutoff := m.now().Add(-dataLifecycleTerminalRetentionAge).Unix()
	terminalCount := 0
	terminalBytes := int64(0)
	terminalArtifactCount := 0
	terminalArtifactBytes := int64(0)
	countedArtifacts := map[string]bool{}
	remove := make([]model.DataLifecycleOperation, 0)
	for _, operation := range operations {
		if !terminalDataLifecycleState(operation.State) || operation.RestoredUntrusted {
			continue
		}
		logicalBytes, err := dataLifecycleOperationLogicalBytes(db.WithContext(ctx), operation)
		if err != nil {
			return result, err
		}
		artifactPath := ""
		artifactBytes := int64(0)
		artifactAddition := false
		if validDigest(operation.BackupRef) {
			artifactPath, artifactBytes, err = m.existingRecoveryArtifact(operation)
			if err != nil && !errors.Is(err, ErrRecoveryBackupUnavailable) {
				return result, err
			}
			if err == nil && !countedArtifacts[filepath.Clean(artifactPath)] {
				artifactAddition = true
			}
		}
		keep := terminalCount < maxDataLifecycleTerminalOperations && operation.UpdatedAt >= cutoff && logicalBytes >= 0 &&
			terminalBytes+logicalBytes <= maxDataLifecycleTerminalLogicalBytes
		if keep && artifactAddition {
			keep = terminalArtifactCount < maxDataLifecycleTerminalArtifacts && artifactBytes >= 0 &&
				terminalArtifactBytes+artifactBytes <= maxDataLifecycleTerminalArtifactBytes
		}
		if !keep {
			remove = append(remove, operation)
			continue
		}
		terminalCount++
		terminalBytes += logicalBytes
		if artifactAddition {
			countedArtifacts[filepath.Clean(artifactPath)] = true
			terminalArtifactCount++
			terminalArtifactBytes += artifactBytes
		}
	}
	if len(remove) > 0 {
		var deletedJournals int64
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for start := 0; start < len(remove); start += 64 {
				end := start + 64
				if end > len(remove) {
					end = len(remove)
				}
				ids := make([]string, 0, end-start)
				for _, operation := range remove[start:end] {
					ids = append(ids, operation.OperationID)
				}
				journalResult := tx.Where("operation_id IN ?", ids).Delete(&model.DataLifecycleJournal{})
				if journalResult.Error != nil {
					return journalResult.Error
				}
				deletedJournals += journalResult.RowsAffected
				if err := tx.Where("operation_id IN ?", ids).Delete(&model.DataLifecycleOperation{}).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.DeletedOperations = len(remove)
		result.DeletedJournals = deletedJournals
	}
	keepPaths, err := m.referencedRecoveryArtifacts(ctx)
	if err != nil {
		return result, err
	}
	files, err := m.sweepOwnedRecoveryFiles(keepPaths)
	result.DeletedArtifacts += files.DeletedArtifacts
	result.DeletedBytes += files.DeletedBytes
	result.PreservedFiles += files.PreservedFiles
	return result, err
}

func terminalDataLifecycleState(state string) bool {
	return state == "APPLIED" || state == "FAILED" || state == "ROLLED_BACK"
}

func dataLifecycleOperationLogicalBytes(db *gorm.DB, operation model.DataLifecycleOperation) (int64, error) {
	encoded, _ := json.Marshal(operation)
	total := int64(len(encoded) + len(operation.IdempotencyKey) + 96)
	var journals []model.DataLifecycleJournal
	if err := db.Where("operation_id = ?", operation.OperationID).Find(&journals).Error; err != nil {
		return 0, err
	}
	for _, journal := range journals {
		payload, _ := json.Marshal(journal)
		total += int64(len(payload) + 48)
	}
	return total, nil
}

func (m *Manager) referencedRecoveryArtifacts(ctx context.Context) (map[string]bool, error) {
	var operations []model.DataLifecycleOperation
	if err := m.database().WithContext(ctx).Where("backup_ref != ''").Find(&operations).Error; err != nil {
		return nil, err
	}
	keep := make(map[string]bool, len(operations))
	for _, operation := range operations {
		if !validDigest(operation.BackupRef) {
			// Invalid authority fails closed: do not sweep an owner path based on
			// malformed metadata that cannot authenticate a file.
			continue
		}
		path, _, err := m.existingRecoveryArtifact(operation)
		if err == nil {
			keep[filepath.Clean(path)] = true
			continue
		}
		if !errors.Is(err, ErrRecoveryBackupUnavailable) {
			return nil, err
		}
	}
	return keep, nil
}

func (m *Manager) removeUnreferencedRecoveryArtifact(ctx context.Context, kind, operationID, backupRef string) error {
	if !validDigest(backupRef) {
		return nil
	}
	var count int64
	if err := m.database().WithContext(ctx).Model(&model.DataLifecycleOperation{}).Where("backup_ref = ?", backupRef).Count(&count).Error; err != nil {
		return err
	}
	if kind == "RESTORE" && count > 0 {
		return nil
	}
	operation := model.DataLifecycleOperation{Kind: kind, OperationID: operationID, BackupRef: backupRef}
	for _, path := range m.recoveryArtifactCandidates(operation) {
		if err := m.filesystem().remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	root := m.dropRecoveryRoot()
	if kind == "RESTORE" {
		root = m.restoreRecoveryRoot()
	}
	return m.filesystem().syncDirectory(root)
}

func (m *Manager) sweepOwnedRecoveryFiles(keep map[string]bool) (PruneResult, error) {
	result := PruneResult{}
	for _, item := range []struct {
		root string
		kind string
	}{{m.dropRecoveryRoot(), "DROP_DATA"}, {m.restoreRecoveryRoot(), "RESTORE"}} {
		entries, err := m.filesystem().readDir(item.root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return result, err
		}
		removed := false
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !ownedRecoveryFilename(item.kind, entry.Name()) {
				result.PreservedFiles++
				continue
			}
			path := filepath.Join(item.root, entry.Name())
			if keep[filepath.Clean(path)] {
				result.PreservedFiles++
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil || !info.Mode().IsRegular() {
				result.PreservedFiles++
				continue
			}
			if err := m.filesystem().remove(path); err != nil {
				return result, err
			}
			removed = true
			result.DeletedArtifacts++
			if info.Size() > 0 {
				result.DeletedBytes += info.Size()
			}
		}
		if removed {
			if err := m.filesystem().syncDirectory(item.root); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func ownedRecoveryFilename(kind, name string) bool {
	switch kind {
	case "DROP_DATA":
		base := strings.TrimSuffix(name, ".partial")
		if strings.HasPrefix(base, "drop-") && strings.HasSuffix(base, ".db") {
			return validDigest(strings.TrimSuffix(strings.TrimPrefix(base, "drop-"), ".db"))
		}
		if strings.HasPrefix(base, "data-operation:") && strings.HasSuffix(base, ".db") {
			identity := strings.TrimSuffix(base, ".db")
			return safeID(identity, 96)
		}
	case "RESTORE":
		if strings.HasPrefix(name, "pre-restore-") && strings.HasSuffix(name, ".partial") {
			return true
		}
		if strings.HasPrefix(name, "pre-restore-") && strings.HasSuffix(name, ".db") {
			return validDigest(strings.TrimSuffix(strings.TrimPrefix(name, "pre-restore-"), ".db"))
		}
	}
	return false
}
