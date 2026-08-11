package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	models := TableModels()
	values := make([]any, 0, len(models))
	for _, table := range models {
		values = append(values, table.Model)
	}
	if err := db.AutoMigrate(values...); err != nil {
		return err
	}
	for _, query := range []string{
		"CREATE INDEX IF NOT EXISTS idx_server_protection_event_resource_time ON server_protection_probe_events(resource_id, observed_at DESC, id DESC)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_score_expiry ON server_protection_score_states(expires_at, resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_signal_scope_time ON server_protection_signals_v2(scope, observed_at DESC, id DESC)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_decision_scope_time ON server_protection_decisions_v2(scope, created_at DESC, id DESC)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_planned_response_expiry ON server_protection_planned_responses_v2(expires_at, id)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_graylist_expiry ON server_protection_graylist(expires_at, resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_graylist_v2_expiry ON server_protection_graylist_v2(lifecycle, expires_at)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_locks_state_heartbeat ON server_protection_operation_locks(state, heartbeat_at)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_artifacts_created ON server_protection_artifacts(created_at DESC, id DESC)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_native_operations_state ON server_protection_native_fallback_operations(workflow_state, updated_at)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_fronting_state_operation ON server_protection_fronting_states_v2(latest_operation_id, latest_operation_revision)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_fronting_receipt_operation ON server_protection_fronting_idempotency_v2(operation_id, operation_revision)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_udp_guard_operation ON server_protection_udp_guard_states_v1(latest_operation_id, latest_operation_revision)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_local_proxy_operation ON server_protection_local_proxy_states_v1(latest_operation_id, latest_operation_revision)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_firewall_contribution_identity ON server_protection_firewall_contributions_v1(network, address_family, resource_id, endpoint_id)",
		"CREATE INDEX IF NOT EXISTS idx_server_protection_firewall_transition_state ON server_protection_firewall_contribution_transitions_v1(state, updated_at)",
	} {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	if err := seedSettings(db); err != nil {
		return err
	}
	if err := db.Model(&SettingsModel{}).Where("id = ? AND revision < ?", 1, 1).Update("revision", 1).Error; err != nil {
		return err
	}
	if err := db.Where("id <> ?", 1).Delete(&SettingsModel{}).Error; err != nil {
		return err
	}
	if err := migrateV2Compatibility(db); err != nil {
		return err
	}
	if err := migrateLegacyGraylistV2(db); err != nil {
		return err
	}
	if err := migrateLegacyFrontingV2(db, time.Now().UTC()); err != nil {
		return err
	}
	if err := quarantineLegacyRecoveryEvidence(db); err != nil {
		return err
	}
	return ReconcileLegacySelfStealProfiles(context.Background(), db, time.Now().UTC())
}

// quarantineLegacyRecoveryEvidence moves compatibility observations only when
// the neutral core schema is present. The optional component knows its legacy
// table; core never names or depends on this component.
func quarantineLegacyRecoveryEvidence(db *gorm.DB) error {
	if !db.Migrator().HasTable(&RecoveryPathModel{}) || !db.Migrator().HasTable("ssh_recovery_evidence_v1") {
		return nil
	}
	return db.Exec(`INSERT OR IGNORE INTO ssh_recovery_evidence_v1
		(id,kind,endpoint_id,principal_id,source_prefix,verification_method,evidence_provider,target_operation,
		 verified_at,expires_at,independence_class,verification_state,operation_bound,single_use,consumed_at,revision,
		 reason_codes_json,source_revision,configuration_revision,service_revision,binary_revision,producer_revision,updated_at)
		SELECT recovery_path_id,kind,endpoint_id,principal_id,source_prefix,verification_method,
		 'legacy-component','',verified_at,expires_at,independence_class,'invalidated',0,0,0,1,
		 '["legacy_evidence_requires_reverification"]',source_revision,configuration_revision,'','',
		 'a7edc2e0c98e65ec144c158337a75d28ce9669c54f58cc7153ae8010276d40ca',verified_at
		FROM server_protection_recovery_paths_v1`).Error
}

func DropSchema(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if err := ensureDropSafe(db); err != nil {
		return err
	}
	models := TableModels()
	for index := len(models) - 1; index >= 0; index-- {
		if !db.Migrator().HasTable(models[index].Model) {
			continue
		}
		if err := db.Migrator().DropTable(models[index].Model); err != nil {
			return err
		}
	}
	return nil
}

func EnsureDropSafe(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	return ensureDropSafe(db)
}

func seedSettings(db *gorm.DB) error {
	defaults := domain.DefaultSettings()
	flags, err := json.Marshal(defaults.FeatureFlags)
	if err != nil {
		return err
	}
	model := settingsModel(defaults, flags)
	model.ID = 1
	return db.Where("id = ?", 1).FirstOrCreate(&model).Error
}

func ensureDropSafe(db *gorm.DB) error {
	if err := ensureFirewallContributionDropSafe(db); err != nil {
		return err
	}
	if err := ensureUDPGuardDropSafe(db); err != nil {
		return err
	}
	if err := ensureFrontingDropSafe(db); err != nil {
		return err
	}
	if err := ensureLocalProxyDropSafe(db); err != nil {
		return err
	}
	if db.Migrator().HasTable(&OperationLockModel{}) {
		var lockCount int64
		dropBlockingLocks := append(NonTerminalOperationLockStates(), "applied", "rollback_failed", "reconcile_required")
		if err := db.Model(&OperationLockModel{}).Where("state IN ?", dropBlockingLocks).Count(&lockCount).Error; err != nil {
			return err
		}
		if lockCount > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d operation locks require recovery or explicit force unlock", lockCount)
		}
	}
	if !db.Migrator().HasTable(&PortOperationModel{}) {
		return ensureNativeDropSafe(db)
	}
	var count int64
	dangerous := []string{"prepared", "applying", "health", "applied", "health_failed", "rolling_back", "rollback_failed", "lock_suspect"}
	if err := db.Model(&PortOperationModel{}).Where("state IN ?", dangerous).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("server-protection data cannot be dropped while %d system operation records require rollback or explicit forget", count)
	}
	return ensureNativeDropSafe(db)
}

func ensureFirewallContributionDropSafe(db *gorm.DB) error {
	if db.Migrator().HasTable(&FirewallContributionModel{}) {
		var count int64
		if err := db.Model(&FirewallContributionModel{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d managed firewall contributions retain authority", count)
		}
	}
	if db.Migrator().HasTable(&FirewallContributionTransitionModel{}) {
		var count int64
		if err := db.Model(&FirewallContributionTransitionModel{}).Where("state IN ?", []string{"PREPARED", "MUTATING", "APPLIED", "HEALTH_VERIFIED", "ROLLING_BACK", "RECOVERY_REQUIRED"}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d firewall contribution transitions retain recovery authority", count)
		}
	}
	if db.Migrator().HasTable(&FirewallCompositionModel{}) {
		var count int64
		if err := db.Model(&FirewallCompositionModel{}).Where("state IN ?", []string{"ACTIVE", "RECOVERY_REQUIRED"}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while managed firewall composition retains authority")
		}
	}
	return nil
}

func ensureUDPGuardDropSafe(db *gorm.DB) error {
	if db.Migrator().HasTable(&UDPGuardStateV1Model{}) {
		var count int64
		guarding := []string{"PREPARED", "APPLIED_EXPERIMENTAL", "DEGRADED", "ROLLING_BACK", "RECOVERY_REQUIRED"}
		if err := db.Model(&UDPGuardStateV1Model{}).Where("actual_state IN ? OR recovery_required = ? OR owns_active_contribution = ? OR recoverable_artifact = ?", guarding, true, true, true).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d UDP guard states retain recovery authority", count)
		}
	}
	if db.Migrator().HasTable(&UDPGuardIdempotencyV1Model{}) {
		var count int64
		if err := db.Model(&UDPGuardIdempotencyV1Model{}).Where("status IN ?", []string{"PENDING", "AMBIGUOUS"}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d UDP guard receipts remain ambiguous", count)
		}
	}
	return nil
}

func ensureFrontingDropSafe(db *gorm.DB) error {
	if db.Migrator().HasTable(&FrontingStateV2Model{}) {
		var states []FrontingStateV2Model
		if err := db.Find(&states).Error; err != nil {
			return err
		}
		for _, state := range states {
			if !ValidFrontingStateV2Model(state) {
				return fmt.Errorf("server-protection data cannot be dropped while fronting semantic state is invalid")
			}
		}
		var count int64
		guarding := []string{"PREPARED", "APPLYING", "HEALTH", "APPLIED", "DEGRADED", "ROLLING_BACK", "ROLLBACK_FAILED", "RECONCILE_REQUIRED"}
		if err := db.Model(&FrontingStateV2Model{}).
			Where("actual_state IN ? OR guarding_provider_lease = ? OR recoverable_artifact = ? OR owns_active_managed_revision = ?", guarding, true, true, true).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d fronting semantic states retain recovery authority", count)
		}
	}
	if db.Migrator().HasTable(&FrontingIdempotencyV2Model{}) {
		var receipts []FrontingIdempotencyV2Model
		if err := db.Find(&receipts).Error; err != nil {
			return err
		}
		for _, receipt := range receipts {
			if !ValidFrontingIdempotencyV2Model(receipt) {
				return fmt.Errorf("server-protection data cannot be dropped while fronting mutation receipt is invalid")
			}
		}
		var count int64
		if err := db.Model(&FrontingIdempotencyV2Model{}).Where("status IN ?", []string{FrontingReceiptPending, FrontingReceiptAmbiguous}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d fronting mutation receipts remain ambiguous", count)
		}
	}
	return nil
}

func ensureLocalProxyDropSafe(db *gorm.DB) error {
	if db.Migrator().HasTable(&LocalProxyStateV1Model{}) {
		var count int64
		guarding := []string{"PREPARED", "APPLYING", "HEALTH", "APPLIED_EXPERIMENTAL", "DEGRADED", "ROLLING_BACK", "RECOVERY_REQUIRED"}
		if err := db.Model(&LocalProxyStateV1Model{}).
			Where("actual_state IN ? OR guarding_provider_lease = ? OR recovery_required = ?", guarding, true, true).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d local proxy guards retain provider authority", count)
		}
	}
	if db.Migrator().HasTable(&LocalProxyIdempotencyV1Model{}) {
		var count int64
		if err := db.Model(&LocalProxyIdempotencyV1Model{}).Where("status IN ?", []string{"PENDING", "AMBIGUOUS"}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d local proxy receipts remain ambiguous", count)
		}
	}
	return nil
}

func ensureNativeDropSafe(db *gorm.DB) error {
	if db.Migrator().HasTable(&NativeFallbackStateModel{}) {
		var count int64
		guarding := []string{"APPLIED", "DEGRADED", "RECONCILE_REQUIRED", "ROLLBACK_FAILED", "PREPARED", "APPLYING", "HEALTH", "ROLLING_BACK"}
		if err := db.Model(&NativeFallbackStateModel{}).Where("actual_state IN ?", guarding).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d native fallback states retain recovery authority", count)
		}
	}
	if db.Migrator().HasTable(&NativeFallbackOperationModel{}) {
		var count int64
		guarding := []string{NativeWorkflowPreparing, NativeWorkflowPrepared, NativeWorkflowApplying, NativeWorkflowHealth, NativeWorkflowApplied, NativeWorkflowRollingBack, NativeWorkflowRollbackFailed, NativeWorkflowReconcileRequired}
		if err := db.Model(&NativeFallbackOperationModel{}).Where("workflow_state IN ? OR (core_checkpoint_id <> '' AND core_checkpoint_released_at IS NULL)", guarding).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d native fallback operations or checkpoints require recovery", count)
		}
	}
	if db.Migrator().HasTable(&FallbackTargetLeaseModel{}) {
		var count int64
		if err := db.Model(&FallbackTargetLeaseModel{}).Where("provider_reservation_id <> '' AND state <> ?", "RELEASED").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("server-protection data cannot be dropped while %d provider reservation mirrors remain guarded", count)
		}
	}
	return nil
}

func settingsModel(value domain.Settings, flags []byte) SettingsModel {
	if flags == nil {
		flags = []byte("{}")
	}
	return SettingsModel{
		Revision:                   1,
		Enabled:                    value.Enabled,
		RetentionGlobalLimit:       value.RetentionGlobalLimit,
		RetentionPerResourceLimit:  value.RetentionPerResourceLimit,
		DefaultScoreThreshold:      value.DefaultScoreThreshold,
		DefaultGraylistTTLSeconds:  value.DefaultGraylistTTLSeconds,
		DiagnosticsCacheTTLSeconds: value.DiagnosticsCacheTTLSeconds,
		ObservationBufferSize:      value.ObservationBufferSize,
		ObservationFlushIntervalMS: value.ObservationFlushIntervalMS,
		IPv6GraylistPrefixBits:     value.IPv6GraylistPrefixBits,
		MaxScore:                   value.MaxScore,
		SafeMetaMaxBytes:           value.SafeMetaMaxBytes,
		ClockSkewToleranceSeconds:  value.ClockSkewToleranceSeconds,
		ArtifactRetentionCount:     value.ArtifactRetentionCount,
		ArtifactRetentionDays:      value.ArtifactRetentionDays,
		AdvancedAcknowledgedAt:     value.AdvancedAcknowledgedAt,
		FeatureFlagsJSON:           flags,
	}
}
