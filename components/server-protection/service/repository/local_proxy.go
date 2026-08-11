package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrLocalProxyIdempotencyConflict = errors.New("local proxy idempotency conflict")

func (r *Repository) SaveLocalProxyState(ctx context.Context, value LocalProxyStateV1Model) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	if value.ResourceID == "" || value.EndpointID == "" || len(value.PlanJSON) == 0 || len(value.HealthJSON) == 0 {
		return errors.New("local proxy state is invalid")
	}
	now := time.Now().UTC().Unix()
	if value.CreatedAt == 0 {
		value.CreatedAt = now
	}
	value.UpdatedAt = now
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "resource_id"}, {Name: "endpoint_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"schema", "actual_state", "apply_gate", "plan_id", "plan_digest", "plan_json", "fact_revision",
			"reference_revision", "lease_id", "lease_revision", "lease_state", "lease_renewed_at", "lease_expires_at", "latest_operation_id",
			"latest_operation_revision", "marker_revision", "health_json", "health_revision",
			"health_expires_unix_nano", "guarding_provider_lease", "recovery_required", "updated_at",
		}),
	}).Create(&value).Error
}

func (r *Repository) LocalProxyStates(ctx context.Context) ([]LocalProxyStateV1Model, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	var values []LocalProxyStateV1Model
	err := r.db.WithContext(ctx).Order("resource_id ASC, endpoint_id ASC").Find(&values).Error
	return values, err
}

func (r *Repository) LocalProxyStateByOperation(ctx context.Context, operationID string) (LocalProxyStateV1Model, error) {
	if r == nil || r.db == nil {
		return LocalProxyStateV1Model{}, errors.New("server-protection repository is not initialized")
	}
	var value LocalProxyStateV1Model
	err := r.db.WithContext(ctx).Where("latest_operation_id = ?", operationID).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return LocalProxyStateV1Model{}, ErrRecordNotFound
	}
	return value, err
}

func ReconcileRestoredLocalProxyRecords(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil || !db.Migrator().HasTable(&LocalProxyStateV1Model{}) {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&LocalProxyStateV1Model{}).
			Where("actual_state IN ? OR guarding_provider_lease = ?", []string{
				"PREPARED", "APPLYING", "HEALTH", "APPLIED_EXPERIMENTAL", "DEGRADED", "ROLLING_BACK",
			}, true).
			Updates(map[string]any{
				"actual_state": "RECOVERY_REQUIRED", "recovery_required": true,
				// Backup contains only a mirror. Never claim restored provider
				// authority until the live provider reconciles it.
				"guarding_provider_lease": false, "updated_at": now.UTC().Unix(),
			}).Error; err != nil {
			return err
		}
		return tx.Model(&LocalProxyIdempotencyV1Model{}).Where("status = ?", "PENDING").
			Updates(map[string]any{"status": "AMBIGUOUS", "updated_at": now.UTC().Unix()}).Error
	})
}

func (r *Repository) BeginLocalProxyReceipt(ctx context.Context, action, key, digest string) (LocalProxyIdempotencyV1Model, bool, error) {
	if r == nil || r.db == nil {
		return LocalProxyIdempotencyV1Model{}, false, errors.New("server-protection repository is not initialized")
	}
	if action == "" || key == "" || len(key) > 128 || len(digest) != 64 {
		return LocalProxyIdempotencyV1Model{}, false, ErrLocalProxyIdempotencyConflict
	}
	now := time.Now().UTC().Unix()
	var result LocalProxyIdempotencyV1Model
	replay := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current LocalProxyIdempotencyV1Model
		err := tx.Where("action = ? AND idempotency_key = ?", action, key).First(&current).Error
		if err == nil {
			if current.RequestDigest != digest {
				return ErrLocalProxyIdempotencyConflict
			}
			if current.Status != "COMPLETE" {
				return fmt.Errorf("%w: receipt is %s", ErrLocalProxyIdempotencyConflict, current.Status)
			}
			result, replay = current, true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		current = LocalProxyIdempotencyV1Model{
			Action: action, IdempotencyKey: key, RequestDigest: digest, Status: "PENDING",
			SemanticResponseJSON: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&current).Error; err != nil {
			return err
		}
		result = current
		return nil
	})
	return result, replay, err
}

func (r *Repository) ReplayLocalProxyReceipt(ctx context.Context, action, key, digest string) (LocalProxyIdempotencyV1Model, bool, error) {
	if r == nil || r.db == nil {
		return LocalProxyIdempotencyV1Model{}, false, errors.New("server-protection repository is not initialized")
	}
	if action == "" || key == "" || len(key) > 128 || len(digest) != 64 {
		return LocalProxyIdempotencyV1Model{}, false, ErrLocalProxyIdempotencyConflict
	}
	var current LocalProxyIdempotencyV1Model
	err := r.db.WithContext(ctx).Where("action = ? AND idempotency_key = ?", action, key).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return LocalProxyIdempotencyV1Model{}, false, nil
	}
	if err != nil {
		return LocalProxyIdempotencyV1Model{}, false, err
	}
	if current.RequestDigest != digest {
		return LocalProxyIdempotencyV1Model{}, false, ErrLocalProxyIdempotencyConflict
	}
	if current.Status != "COMPLETE" {
		return LocalProxyIdempotencyV1Model{}, false,
			fmt.Errorf("%w: receipt is %s", ErrLocalProxyIdempotencyConflict, current.Status)
	}
	return current, true, nil
}

func (r *Repository) CompleteLocalProxyReceipt(ctx context.Context, id uint, operationID string, operationRevision int, response any) error {
	content, err := json.Marshal(response)
	if err != nil || len(content) > 64<<10 {
		return ErrLocalProxyIdempotencyConflict
	}
	update := r.db.WithContext(ctx).Model(&LocalProxyIdempotencyV1Model{}).
		Where("id = ? AND status = ?", id, "PENDING").
		Updates(map[string]any{
			"operation_id": operationID, "operation_revision": operationRevision, "status": "COMPLETE",
			"semantic_response_json": content, "updated_at": time.Now().UTC().Unix(),
		})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrLocalProxyIdempotencyConflict
	}
	return nil
}

func (r *Repository) AmbiguousLocalProxyReceipt(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&LocalProxyIdempotencyV1Model{}).
		Where("id = ? AND status = ?", id, "PENDING").
		Updates(map[string]any{"status": "AMBIGUOUS", "updated_at": time.Now().UTC().Unix()}).Error
}
