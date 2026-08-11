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

var ErrUDPGuardIdempotencyConflict = errors.New("udp guard idempotency conflict")

func (r *Repository) SaveUDPGuardState(ctx context.Context, value UDPGuardStateV1Model) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	now := time.Now().UTC().Unix()
	if value.CreatedAt == 0 {
		value.CreatedAt = now
	}
	value.UpdatedAt = now
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if value.OwnsActiveContribution && value.ContributionID != "" {
			if err := tx.Model(&UDPGuardStateV1Model{}).
				Where("contribution_id = ? AND NOT (resource_id = ? AND endpoint_id = ?)", value.ContributionID, value.ResourceID, value.EndpointID).
				Updates(map[string]any{"actual_state": "NOT_APPLIED", "recovery_required": false, "owns_active_contribution": false, "recoverable_artifact": false, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "resource_id"}, {Name: "endpoint_id"}}, DoUpdates: clause.AssignmentColumns([]string{"address_family", "schema", "desired_policy", "selected_strategy", "actual_state", "plan_id", "plan_digest", "capability_revision", "claim_revision", "policy_revision", "contribution_id", "contribution_revision", "composition_revision", "managed_plan_revision", "health_provider_instance", "health_generation", "health_observation_revision", "health_started_unix_nano", "health_completed_unix_nano", "health_expires_unix_nano", "latest_operation_id", "latest_operation_revision", "recovery_required", "owns_active_contribution", "recoverable_artifact", "updated_at"})}).Create(&value).Error
	})
}

func (r *Repository) UDPGuardStates(ctx context.Context) ([]UDPGuardStateV1Model, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	var values []UDPGuardStateV1Model
	err := r.db.WithContext(ctx).Order("resource_id ASC, endpoint_id ASC").Find(&values).Error
	return values, err
}

func ReconcileRestoredUDPGuardRecords(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil || !db.Migrator().HasTable(&UDPGuardStateV1Model{}) {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&UDPGuardStateV1Model{}).Where("actual_state IN ?", []string{"PREPARED", "APPLIED_EXPERIMENTAL", "DEGRADED", "ROLLING_BACK"}).Updates(map[string]any{"actual_state": "RECOVERY_REQUIRED", "recovery_required": true, "owns_active_contribution": false, "updated_at": now.UTC().Unix()}).Error; err != nil {
			return err
		}
		return tx.Model(&UDPGuardIdempotencyV1Model{}).Where("status = ?", "PENDING").Updates(map[string]any{"status": "AMBIGUOUS", "updated_at": now.UTC().Unix()}).Error
	})
}

func (r *Repository) BeginUDPGuardReceipt(ctx context.Context, action, key, digest string) (UDPGuardIdempotencyV1Model, bool, error) {
	if r == nil || r.db == nil {
		return UDPGuardIdempotencyV1Model{}, false, errors.New("server-protection repository is not initialized")
	}
	if action == "" || key == "" || len(key) > 128 || len(digest) != 64 {
		return UDPGuardIdempotencyV1Model{}, false, ErrUDPGuardIdempotencyConflict
	}
	now := time.Now().UTC().Unix()
	var result UDPGuardIdempotencyV1Model
	replay := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current UDPGuardIdempotencyV1Model
		err := tx.Where("action = ? AND idempotency_key = ?", action, key).First(&current).Error
		if err == nil {
			if current.RequestDigest != digest {
				return ErrUDPGuardIdempotencyConflict
			}
			if current.Status != "COMPLETE" {
				return fmt.Errorf("%w: receipt is %s", ErrUDPGuardIdempotencyConflict, current.Status)
			}
			result = current
			replay = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		current = UDPGuardIdempotencyV1Model{Action: action, IdempotencyKey: key, RequestDigest: digest, Status: "PENDING", SemanticResponseJSON: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&current).Error; err != nil {
			return err
		}
		result = current
		return nil
	})
	return result, replay, err
}

func (r *Repository) CompleteUDPGuardReceipt(ctx context.Context, id uint, operationID string, operationRevision int, response any) error {
	content, err := json.Marshal(response)
	if err != nil || len(content) > 32<<10 {
		return ErrUDPGuardIdempotencyConflict
	}
	update := r.db.WithContext(ctx).Model(&UDPGuardIdempotencyV1Model{}).Where("id = ? AND status = ?", id, "PENDING").Updates(map[string]any{"operation_id": operationID, "operation_revision": operationRevision, "status": "COMPLETE", "semantic_response_json": content, "updated_at": time.Now().UTC().Unix()})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrUDPGuardIdempotencyConflict
	}
	return nil
}

func (r *Repository) AmbiguousUDPGuardReceipt(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&UDPGuardIdempotencyV1Model{}).Where("id = ? AND status = ?", id, "PENDING").Updates(map[string]any{"status": "AMBIGUOUS", "updated_at": time.Now().UTC().Unix()}).Error
}
