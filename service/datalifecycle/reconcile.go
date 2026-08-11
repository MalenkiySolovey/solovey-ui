package datalifecycle

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

const maxStartupReconciliations = 32

// ReconcileStartup resolves operations whose process was interrupted. Restore
// file recovery runs before database bootstrap; therefore an in-flight restore
// row means the exact fallback is active again. Drop Data is only auto-closed
// before owner mutation begins—ambiguous partial deletion requires recovery.
func (m *Manager) ReconcileStartup(ctx context.Context) error {
	if m == nil || ctx == nil {
		return errors.New("data lifecycle startup reconciliation is unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	db := m.database()
	if db == nil {
		return errors.New("data lifecycle database unavailable")
	}
	var operations []model.DataLifecycleOperation
	if err := db.WithContext(ctx).
		Where("state NOT IN ? OR restored_untrusted = ?", []string{"APPLIED", "FAILED", "ROLLED_BACK"}, true).
		Order("updated_at ASC, operation_id ASC").Limit(maxStartupReconciliations + 1).Find(&operations).Error; err != nil {
		return err
	}
	truncated := len(operations) > maxStartupReconciliations
	if truncated {
		operations = operations[:maxStartupReconciliations]
	}
	for _, operation := range operations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validPersistedDataOperation(operation) {
			return errors.New("data lifecycle operation authority is invalid")
		}
		state, event, reason := startupDisposition(operation)
		if operation.State == state && operation.ReasonCode == reason {
			continue
		}
		if _, err := m.advance(ctx, operation, state, event, reason); err != nil {
			return err
		}
	}
	if truncated {
		return errors.New("data lifecycle startup reconciliation inventory exceeded its bound")
	}
	return nil
}

func startupDisposition(operation model.DataLifecycleOperation) (string, string, string) {
	if operation.RestoredUntrusted {
		return "RECOVERY_REQUIRED", "restored_data_lifecycle_state_requires_recovery", "restored_data_lifecycle_state_untrusted"
	}
	switch operation.Kind {
	case "RESTORE":
		switch operation.State {
		case "ADMITTED", "RESTORING":
			return "ROLLED_BACK", "interrupted_restore_rolled_back", "restore_interrupted_exact_fallback_active"
		default:
			return "RECOVERY_REQUIRED", "restore_state_requires_recovery", "restore_state_invalid"
		}
	case "DROP_DATA":
		switch operation.State {
		case "ADMITTED", "BACKING_UP", "BACKUP_READY":
			return "ROLLED_BACK", "interrupted_drop_closed_before_mutation", "drop_interrupted_before_owner_mutation"
		case "DROPPING", "VERIFYING":
			return "RECOVERY_REQUIRED", "interrupted_drop_requires_recovery", "drop_interrupted_after_owner_mutation"
		default:
			return "RECOVERY_REQUIRED", "drop_state_requires_recovery", "drop_state_invalid"
		}
	default:
		return "RECOVERY_REQUIRED", "data_lifecycle_state_requires_recovery", "data_lifecycle_kind_invalid"
	}
}
