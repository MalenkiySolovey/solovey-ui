package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxFrontingSemanticBlobV2 = 64 << 10

const (
	FrontingStateSchemaV2                = "solovey-ui/fronting-semantic-state/v2"
	FrontingCompatibilityCurrentV2       = "CURRENT_V2"
	FrontingCompatibilityLegacyRepreview = "LEGACY_V1_REPREVIEW_REQUIRED"
	FrontingReceiptPending               = "PENDING"
	FrontingReceiptComplete              = "COMPLETE"
	FrontingReceiptAmbiguous             = "AMBIGUOUS"
)

func (r *Repository) FrontingStatesV2(ctx context.Context) ([]FrontingStateV2Model, error) {
	query, err := r.query(ctx, &FrontingStateV2Model{})
	if err != nil {
		return nil, err
	}
	var values []FrontingStateV2Model
	err = query.Order("resource_id ASC").Find(&values).Error
	return values, err
}

func (r *Repository) FrontingStateV2(ctx context.Context, resourceID string) (FrontingStateV2Model, error) {
	query, err := r.query(ctx, &FrontingStateV2Model{})
	if err != nil {
		return FrontingStateV2Model{}, err
	}
	var value FrontingStateV2Model
	err = query.Where("resource_id = ?", resourceID).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FrontingStateV2Model{}, ErrRecordNotFound
	}
	return value, err
}

// ProjectFrontingStateV2 accepts only a complete service-authored projection.
// Callers cannot patch ActualState independently, preventing a generic store
// method from manufacturing APPLIED.
func (r *Repository) ProjectFrontingStateV2(ctx context.Context, value FrontingStateV2Model) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	if !ValidFrontingStateV2Model(value) {
		return errors.New("fronting semantic projection is invalid")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "resource_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"schema", "display_identity", "desired_strategy", "selected_strategy", "actual_state", "apply_gate",
			"runtime_state", "installation_class", "runtime_identity_revision", "strategy_capability_revision",
			"socket_claim_json", "backend_references_json", "fallback_references_json", "selector_set_json", "lease_mirrors_json",
			"default_policy", "selected_proxy_mode", "active_map_revision", "candidate_revision", "active_revision",
			"latest_operation_id", "latest_operation_revision", "latest_operation_state", "health_state", "health_observed_at",
			"health_expires_at", "recovery_classification", "compatibility_state", "reason_codes_json", "blocks_json",
			"warnings_json", "safe_next_action", "guarding_provider_lease", "recoverable_artifact",
			"owns_active_managed_revision", "updated_at",
		}),
	}).Create(&value).Error
}

// ClaimFrontingReceiptV2 returns the existing receipt when the key has already
// been claimed. The caller must compare RequestDigest before replaying it.
func (r *Repository) ClaimFrontingReceiptV2(ctx context.Context, value FrontingIdempotencyV2Model) (FrontingIdempotencyV2Model, bool, error) {
	if r == nil || r.db == nil {
		return FrontingIdempotencyV2Model{}, false, errors.New("server-protection repository is not initialized")
	}
	if !ValidFrontingIdempotencyV2Model(value) || value.Status != FrontingReceiptPending {
		return FrontingIdempotencyV2Model{}, false, errors.New("fronting idempotency receipt is invalid")
	}
	var result FrontingIdempotencyV2Model
	joined := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("action = ? AND idempotency_key = ?", value.Action, value.IdempotencyKey).First(&result).Error
		if err == nil {
			if !ValidFrontingIdempotencyV2Model(result) {
				return errors.New("fronting idempotency receipt is invalid")
			}
			joined = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(&value).Error; err != nil {
			return err
		}
		result = value
		return nil
	})
	return result, joined, err
}

func (r *Repository) FrontingReceiptV2(ctx context.Context, action, key string) (FrontingIdempotencyV2Model, error) {
	if r == nil || r.db == nil {
		return FrontingIdempotencyV2Model{}, errors.New("server-protection repository is not initialized")
	}
	var value FrontingIdempotencyV2Model
	err := r.db.WithContext(ctx).Where("action = ? AND idempotency_key = ?", action, key).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FrontingIdempotencyV2Model{}, ErrRecordNotFound
	}
	if err == nil && !ValidFrontingIdempotencyV2Model(value) {
		return FrontingIdempotencyV2Model{}, errors.New("fronting idempotency receipt is invalid")
	}
	return value, err
}

func (r *Repository) CompleteFrontingReceiptV2(ctx context.Context, action, key, digest string, operationID string, operationRevision int, response []byte, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	if (action != "apply" && action != "rollback") || !frontingOpaqueV2(key, 128) || !frontingDigestV2(digest) ||
		!frontingOpaqueV2(operationID, 128) || operationRevision <= 0 || len(response) == 0 || len(response) > maxFrontingSemanticBlobV2 || !json.Valid(response) || now <= 0 {
		return errors.New("fronting idempotency receipt is invalid")
	}
	result := r.db.WithContext(ctx).Model(&FrontingIdempotencyV2Model{}).
		Where("action = ? AND idempotency_key = ? AND request_digest = ? AND status = ?", action, key, digest, FrontingReceiptPending).
		Updates(map[string]any{"operation_id": operationID, "operation_revision": operationRevision, "status": FrontingReceiptComplete, "response_json": response, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrRevisionConflict
	}
	return nil
}

// ReconcileRestoredFrontingRecords distrusts imported active-like projections
// and pending receipts. It performs database-only classification and never
// calls providers, helpers, switch/reload, or lease mutation.
func ReconcileRestoredFrontingRecords(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil || !db.Migrator().HasTable(&FrontingStateV2Model{}) {
		return nil
	}
	guarding := []string{"PREPARED", "APPLYING", "HEALTH", "APPLIED", "DEGRADED", "ROLLING_BACK", "ROLLBACK_FAILED", "RECONCILE_REQUIRED"}
	if err := db.WithContext(ctx).Model(&FrontingStateV2Model{}).
		Where("actual_state IN ? AND actual_state <> ?", guarding, "RECONCILE_REQUIRED").
		Updates(map[string]any{
			"actual_state": "RECONCILE_REQUIRED", "recovery_classification": "RESTORED_STATE_UNVERIFIED",
			"safe_next_action": "INSPECT_RECOVERY", "owns_active_managed_revision": false, "updated_at": now.UTC().Unix(),
		}).Error; err != nil {
		return err
	}
	var states []FrontingStateV2Model
	if err := db.WithContext(ctx).Find(&states).Error; err != nil {
		return err
	}
	for _, state := range states {
		if ValidFrontingStateV2Model(state) || state.ActualState == "RECONCILE_REQUIRED" && state.RecoveryClassification == "RESTORED_STATE_INVALID" {
			continue
		}
		if err := db.WithContext(ctx).Model(&FrontingStateV2Model{}).Where("resource_id = ?", state.ResourceID).Updates(map[string]any{
			"actual_state": "RECONCILE_REQUIRED", "recovery_classification": "RESTORED_STATE_INVALID",
			"safe_next_action": "INSPECT_RECOVERY", "owns_active_managed_revision": false, "updated_at": now.UTC().Unix(),
		}).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasTable(&FrontingIdempotencyV2Model{}) {
		return nil
	}
	return db.WithContext(ctx).Model(&FrontingIdempotencyV2Model{}).
		Where("status = ?", FrontingReceiptPending).
		Updates(map[string]any{"status": FrontingReceiptAmbiguous, "updated_at": now.UTC().Unix()}).Error
}

// ValidFrontingStateV2Model is deliberately structural and bounded. Runtime
// semantic validation remains in the fronting service, while restore,
// replay, and destructive guards can all fail closed on malformed storage.
func ValidFrontingStateV2Model(value FrontingStateV2Model) bool {
	if value.ResourceID == "" || len(value.ResourceID) > 256 || value.Schema != FrontingStateSchemaV2 ||
		!frontingStateDesiredV2(value.DesiredStrategy) || !frontingStateSelectedV2(value.SelectedStrategy) || !frontingStateActualV2(value.ActualState) ||
		value.ApplyGate != "EXPERIMENTAL_DISABLED_BY_DEFAULT" || !frontingRuntimeStateV2(value.RuntimeState) || !frontingInstallationClassV2(value.InstallationClass) ||
		(value.CompatibilityState != FrontingCompatibilityCurrentV2 && value.CompatibilityState != FrontingCompatibilityLegacyRepreview) ||
		value.CreatedAt <= 0 || value.UpdatedAt <= 0 || value.UpdatedAt < value.CreatedAt {
		return false
	}
	for _, data := range []json.RawMessage{value.SocketClaimJSON, value.BackendReferencesJSON, value.FallbackReferencesJSON, value.SelectorSetJSON, value.LeaseMirrorsJSON, value.ReasonCodesJSON, value.BlocksJSON, value.WarningsJSON} {
		if len(data) == 0 || len(data) > maxFrontingSemanticBlobV2 || !json.Valid(data) {
			return false
		}
	}
	return true
}

func ValidFrontingIdempotencyV2Model(value FrontingIdempotencyV2Model) bool {
	if (value.Action != "apply" && value.Action != "rollback") || !frontingOpaqueV2(value.IdempotencyKey, 128) || !frontingDigestV2(value.RequestDigest) ||
		(value.Status != FrontingReceiptPending && value.Status != FrontingReceiptComplete && value.Status != FrontingReceiptAmbiguous) ||
		len(value.ResponseJSON) == 0 || len(value.ResponseJSON) > maxFrontingSemanticBlobV2 || !json.Valid(value.ResponseJSON) ||
		value.CreatedAt <= 0 || value.UpdatedAt <= 0 || value.UpdatedAt < value.CreatedAt {
		return false
	}
	if value.Status == FrontingReceiptComplete {
		return frontingOpaqueV2(value.OperationID, 128) && value.OperationRevision > 0
	}
	return value.OperationID == "" && value.OperationRevision == 0
}

func frontingStateDesiredV2(value string) bool {
	return value == "DISABLED" || value == "DISABLED_REPREVIEW_REQUIRED" || value == "L4_ONE_TO_ONE_FRONTING" || value == "SNI_PREREAD_FRONTING"
}

func frontingStateSelectedV2(value string) bool {
	return value == "" || value == "L4_ONE_TO_ONE_FRONTING" || value == "SNI_PREREAD_FRONTING"
}

func frontingStateActualV2(value string) bool {
	switch value {
	case "NOT_APPLIED", "PREPARED", "APPLYING", "HEALTH", "APPLIED", "DEGRADED", "ROLLING_BACK", "ROLLED_BACK", "ROLLBACK_FAILED", "RECONCILE_REQUIRED", "CANCELLED":
		return true
	default:
		return false
	}
}

func frontingRuntimeStateV2(value string) bool {
	switch value {
	case "UNKNOWN", "NGINX_NOT_INSTALLED", "NGINX_EXTERNAL_MANAGED", "NGINX_IDENTITY_UNKNOWN", "STREAM_UNAVAILABLE", "SSL_PREREAD_UNAVAILABLE", "PROXY_PROTOCOL_UNPROVEN", "VALIDATION_UNAVAILABLE", "RELOAD_UNAVAILABLE", "MANAGED_ENGINE_READY":
		return true
	default:
		return false
	}
}

func frontingInstallationClassV2(value string) bool {
	return value == "UNKNOWN" || value == "SOLOVEY_MANAGED" || value == "EXTERNAL_MANAGED" || value == "DEVELOPMENT_READ_ONLY"
}

func frontingOpaqueV2(value string, limit int) bool {
	if value == "" || len(value) > limit || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func frontingDigestV2(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return true
}

func migrateLegacyFrontingV2(db *gorm.DB, now time.Time) error {
	if db == nil || !db.Migrator().HasTable(&OperationLockModel{}) || !db.Migrator().HasTable(&FrontingStateV2Model{}) {
		return nil
	}
	var operations []OperationLockModel
	if err := db.Where("kind = ?", "fronting").Order("created_at ASC, id ASC").Find(&operations).Error; err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.ResourceID == "" {
			continue
		}
		value := FrontingStateV2Model{
			ResourceID: operation.ResourceID, Schema: FrontingStateSchemaV2, DisplayIdentity: operation.ResourceID,
			DesiredStrategy: "DISABLED_REPREVIEW_REQUIRED", ActualState: "NOT_APPLIED",
			ApplyGate: "EXPERIMENTAL_DISABLED_BY_DEFAULT", RuntimeState: "UNKNOWN", InstallationClass: "UNKNOWN",
			SocketClaimJSON: []byte(`{}`), BackendReferencesJSON: []byte(`[]`), FallbackReferencesJSON: []byte(`[]`),
			SelectorSetJSON: []byte(`{}`), LeaseMirrorsJSON: []byte(`[]`),
			LatestOperationID: operation.OperationID, LatestOperationRevision: operation.Revision, LatestOperationState: operation.State,
			CompatibilityState: FrontingCompatibilityLegacyRepreview, ReasonCodesJSON: []byte(`["legacy_fronting_requires_v2_preview"]`),
			BlocksJSON: []byte(`["legacy_fronting_requires_v2_preview"]`), WarningsJSON: []byte(`[]`), SafeNextAction: "PREVIEW",
			CreatedAt: now.UTC().Unix(), UpdatedAt: now.UTC().Unix(),
		}
		if err := db.Where("resource_id = ?", value.ResourceID).FirstOrCreate(&value).Error; err != nil {
			return err
		}
	}
	return nil
}
