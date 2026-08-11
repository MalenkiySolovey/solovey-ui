package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrOperationConflict = errors.New("another server-protection system operation is active")
	ErrOperationFenced   = errors.New("server-protection operation ownership changed")
)

var nonTerminalOperationLockStates = []string{
	"prepared", "applying", "health", "health_failed", "rolling_back", "lock_suspect",
}

func NonTerminalOperationLockStates() []string {
	return append([]string(nil), nonTerminalOperationLockStates...)
}

type AcquireOperationLockInput struct {
	OperationID        string
	Kind               string
	ResourceID         string
	Protocol           string
	Listen             string
	Port               *int
	State              string
	IdempotencyKey     string
	PlanRevision       string
	HelperRevision     string
	LockedByPID        int
	LockedByInstanceID string
	Actor              string
	Now                int64
	ExpiresAt          int64
}

func (r *Repository) AcquireOperationLock(ctx context.Context, input AcquireOperationLockInput) (OperationLockModel, bool, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, false, errors.New("server-protection repository is not initialized")
	}
	var result OperationLockModel
	joined := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.IdempotencyKey != "" {
			err := tx.Where("idempotency_key = ?", input.IdempotencyKey).Order("id DESC").First(&result).Error
			if err == nil {
				joined = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		var active int64
		if err := tx.Model(&OperationLockModel{}).Where("state IN ?", nonTerminalOperationLockStates).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrOperationConflict
		}

		pid := input.LockedByPID
		result = OperationLockModel{
			OperationID: input.OperationID, Kind: input.Kind, ResourceID: input.ResourceID,
			Protocol: input.Protocol, Listen: input.Listen, Port: input.Port, State: input.State,
			Revision: 1, IdempotencyKey: input.IdempotencyKey, LockedByPID: &pid,
			PlanRevision:       input.PlanRevision,
			HelperRevision:     input.HelperRevision,
			LockedByInstanceID: input.LockedByInstanceID, Actor: input.Actor,
			HeartbeatAt: input.Now, ExpiresAt: input.ExpiresAt, CreatedAt: input.Now, UpdatedAt: input.Now,
		}
		return tx.Create(&result).Error
	})
	return result, joined, err
}

func (r *Repository) OperationByIdempotencyKey(ctx context.Context, key string) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Where("idempotency_key = ?", key).Order("id DESC").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return OperationLockModel{}, ErrRecordNotFound
	}
	return item, err
}

func (r *Repository) OperationByID(ctx context.Context, operationID string) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return OperationLockModel{}, ErrRecordNotFound
	}
	return item, err
}

func (r *Repository) OperationByHelperRevisionPrefix(ctx context.Context, prefix string) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Where("helper_revision LIKE ?", prefix+"%").Order("id DESC").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return OperationLockModel{}, ErrRecordNotFound
	}
	return item, err
}

func (r *Repository) ListOperationLocks(ctx context.Context, states []string) ([]OperationLockModel, error) {
	query, err := r.query(ctx, &OperationLockModel{})
	if err != nil {
		return nil, err
	}
	if len(states) > 0 {
		query = query.Where("state IN ?", states)
	}
	var items []OperationLockModel
	err = query.Order("created_at DESC, id DESC").Find(&items).Error
	return items, err
}

type FencedOperationLockUpdate struct {
	OperationID    string
	Revision       int
	InstanceID     string
	PID            int
	FromStates     []string
	ToState        string
	HelperRevision *string
	Now            int64
	ExpiresAt      int64
}

func (r *Repository) UpdateOperationLockFenced(ctx context.Context, update FencedOperationLockUpdate) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]any{"state": update.ToState, "revision": update.Revision + 1, "heartbeat_at": update.Now, "expires_at": update.ExpiresAt, "updated_at": update.Now}
		if update.HelperRevision != nil {
			values["helper_revision"] = *update.HelperRevision
		}
		result := tx.Model(&OperationLockModel{}).
			Where("operation_id = ? AND revision = ? AND locked_by_instance_id = ? AND locked_by_pid = ? AND state IN ?", update.OperationID, update.Revision, update.InstanceID, update.PID, update.FromStates).
			Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOperationFenced
		}
		return tx.Where("operation_id = ?", update.OperationID).First(&item).Error
	})
	return item, err
}

func (r *Repository) HeartbeatOperationLock(ctx context.Context, operationID, instanceID string, pid, revision int, now, expiresAt int64) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&OperationLockModel{}).
			Where("operation_id = ? AND revision = ? AND locked_by_instance_id = ? AND locked_by_pid = ? AND state IN ?", operationID, revision, instanceID, pid, nonTerminalOperationLockStates).
			Updates(map[string]any{"heartbeat_at": now, "expires_at": expiresAt, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOperationFenced
		}
		return tx.Where("operation_id = ?", operationID).First(&item).Error
	})
	return item, err
}

type RecoveryOperationLockUpdate struct {
	OperationID      string
	ExpectedRevision int
	FromStates       []string
	ToState          string
	Now              int64
}

type ReclaimOperationLockUpdate struct {
	OperationID    string
	Revision       int
	InstanceID     string
	PID            int
	FromState      string
	ToState        string
	HelperRevision *string
	Now            int64
	ExpiresAt      int64
}

// ReclaimOperationLock creates a fresh owner/revision fence before a manual
// rollback or kind-specific restart reconciler can invoke its backend. The
// caller supplies one exact current state; the CAS never upgrades a lock.
func (r *Repository) ReclaimOperationLock(ctx context.Context, update ReclaimOperationLockUpdate) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]any{"state": update.ToState, "revision": update.Revision + 1, "locked_by_instance_id": update.InstanceID, "locked_by_pid": update.PID, "heartbeat_at": update.Now, "expires_at": update.ExpiresAt, "updated_at": update.Now}
		if update.HelperRevision != nil {
			values["helper_revision"] = *update.HelperRevision
		}
		result := tx.Model(&OperationLockModel{}).Where("operation_id = ? AND revision = ? AND state = ?", update.OperationID, update.Revision, update.FromState).
			Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOperationFenced
		}
		return tx.Where("operation_id = ?", update.OperationID).First(&item).Error
	})
	return item, err
}

func (r *Repository) RecoverOperationLock(ctx context.Context, update RecoveryOperationLockUpdate) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&OperationLockModel{}).
			Where("operation_id = ? AND revision = ? AND state IN ?", update.OperationID, update.ExpectedRevision, update.FromStates).
			Updates(map[string]any{"state": update.ToState, "revision": update.ExpectedRevision + 1, "updated_at": update.Now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return tx.Where("operation_id = ?", update.OperationID).First(&item).Error
	})
	return item, err
}

func (r *Repository) ForceUnlockOperation(ctx context.Context, operationID string, expectedRevision int, now int64) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current OperationLockModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("operation_id = ?", operationID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRecordNotFound
			}
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if !containsOperationState(nonTerminalOperationLockStates, current.State) {
			return ErrOperationConflict
		}
		result := tx.Model(&OperationLockModel{}).Where("operation_id = ? AND revision = ?", operationID, expectedRevision).
			Updates(map[string]any{"state": "force_unlocked", "revision": expectedRevision + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return tx.Where("operation_id = ?", operationID).First(&item).Error
	})
	return item, err
}

func (r *Repository) ForgetOperationState(ctx context.Context, operationID string, expectedRevision int, now int64) (OperationLockModel, error) {
	if r == nil || r.db == nil {
		return OperationLockModel{}, errors.New("server-protection repository is not initialized")
	}
	var item OperationLockModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current OperationLockModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("operation_id = ?", operationID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRecordNotFound
			}
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if current.State != "applied" && current.State != "rollback_failed" {
			return ErrOperationConflict
		}
		result := tx.Model(&OperationLockModel{}).Where("operation_id = ? AND revision = ?", operationID, expectedRevision).
			Updates(map[string]any{"state": "forgotten", "revision": expectedRevision + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		if err := tx.Model(&PortOperationModel{}).Where("operation_id = ? AND state IN ?", operationID, []string{"applied", "rollback_failed"}).
			Updates(map[string]any{"state": "forgotten", "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Where("operation_id = ?", operationID).First(&item).Error
	})
	return item, err
}

func containsOperationState(states []string, state string) bool {
	for _, candidate := range states {
		if candidate == state {
			return true
		}
	}
	return false
}
