package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestNativeOperationAndReservationMirrorMigrationBackupOwnership(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"server_protection_native_fallback_operations": false,
		"server_protection_native_fallback_states":     false,
		"server_protection_fallback_target_leases":     false,
	}
	for _, table := range TableModels() {
		if _, ok := want[table.Name]; ok {
			want[table.Name] = db.Migrator().HasTable(table.Model)
		}
	}
	for _, table := range BackupTableModels() {
		if _, ok := want[table.Name]; ok && !db.Migrator().HasTable(table.Model) {
			t.Fatalf("backup-owned table %s is not migrated", table.Name)
		}
		delete(want, table.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing native backup ownership: %#v", want)
	}
	if _, ok := reflect.TypeOf(&Repository{}).MethodByName("SaveNativeFallbackState"); ok {
		t.Fatal("generic native actual-state writer exists")
	}
}

func TestNativeDropSafetyGuardsEveryAuthorityAndAllowsVerifiedCleanup(t *testing.T) {
	for _, actual := range []domain.NativeFallbackActualState{
		domain.NativeActualPrepared, domain.NativeActualApplying, domain.NativeActualHealth, domain.NativeActualApplied,
		domain.NativeActualDegraded, domain.NativeActualRollingBack, domain.NativeActualRollbackFailed, domain.NativeActualReconcileRequired,
	} {
		t.Run("state_"+strings.ToLower(string(actual)), func(t *testing.T) {
			db := openTestDB(t)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			row := nativeFallbackStateRow(actual)
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if err := EnsureDropSafe(db); err == nil {
				t.Fatalf("drop accepted guarding native state %s", actual)
			}
		})
	}

	for _, workflowState := range []string{
		NativeWorkflowPreparing, NativeWorkflowPrepared, NativeWorkflowApplying, NativeWorkflowHealth,
		NativeWorkflowApplied, NativeWorkflowRollingBack, NativeWorkflowRollbackFailed, NativeWorkflowReconcileRequired,
	} {
		t.Run("operation_"+workflowState, func(t *testing.T) {
			db := openTestDB(t)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			row := nativeOperationGuardRow(workflowState)
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if err := EnsureDropSafe(db); err == nil {
				t.Fatalf("drop accepted guarding workflow state %s", workflowState)
			}
		})
	}

	t.Run("recoverable_checkpoint", func(t *testing.T) {
		db := openTestDB(t)
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
		row := nativeOperationGuardRow(NativeWorkflowRolledBack)
		row.CoreCheckpointID = "checkpoint-retained"
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := EnsureDropSafe(db); err == nil {
			t.Fatal("drop accepted a recoverable core checkpoint")
		}
	})

	t.Run("provider_authority", func(t *testing.T) {
		db := openTestDB(t)
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
		mirror := nativeMirrorGuardRow("RESERVED")
		if err := db.Create(&mirror).Error; err != nil {
			t.Fatal(err)
		}
		if err := EnsureDropSafe(db); err == nil {
			t.Fatal("drop accepted a non-released provider mirror")
		}
	})

	t.Run("verified_terminal_cleanup", func(t *testing.T) {
		db := openTestDB(t)
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
		state := nativeFallbackStateRow(domain.NativeActualRolledBack)
		operation := nativeOperationGuardRow(NativeWorkflowRolledBack)
		mirror := nativeMirrorGuardRow("RELEASED")
		if err := db.Create(&state).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&operation).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&mirror).Error; err != nil {
			t.Fatal(err)
		}
		if err := EnsureDropSafe(db); err != nil {
			t.Fatalf("verified terminal cleanup remained blocked: %v", err)
		}
	})
}

func TestRestoredNativeOperationTruthBecomesUntrustedIdempotently(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	operation := nativeOperationGuardRow(NativeWorkflowApplied)
	operation.Revision = 7
	state := nativeFallbackStateRow(domain.NativeActualApplied)
	state.OperationID = operation.OperationID
	state.OperationRevision = "7"
	mirror := nativeMirrorGuardRow("ACTIVE")
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mirror).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_910_000_000, 0).UTC()
	if err := ReconcileRestoredNativeFallbackRecords(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	var restoredOperation NativeFallbackOperationModel
	var restoredState NativeFallbackStateModel
	var restoredMirror FallbackTargetLeaseModel
	if err := db.Where("operation_id = ?", operation.OperationID).First(&restoredOperation).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("resource_id = ?", state.ResourceID).First(&restoredState).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("lease_id = ?", mirror.LeaseID).First(&restoredMirror).Error; err != nil {
		t.Fatal(err)
	}
	if restoredOperation.WorkflowState != NativeWorkflowReconcileRequired || restoredOperation.Revision != 8 ||
		restoredOperation.RecoveryClassification != "restored_state_untrusted" || restoredState.ActualState != string(domain.NativeActualReconcileRequired) ||
		restoredMirror.State != "ACTIVE" {
		t.Fatalf("restore did not fail closed: operation=%#v state=%#v mirror=%#v", restoredOperation, restoredState, restoredMirror)
	}
	updatedAt := restoredOperation.UpdatedAt
	stateUpdatedAt := restoredState.UpdatedAt
	if err := ReconcileRestoredNativeFallbackRecords(context.Background(), db, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("operation_id = ?", operation.OperationID).First(&restoredOperation).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("resource_id = ?", state.ResourceID).First(&restoredState).Error; err != nil {
		t.Fatal(err)
	}
	if restoredOperation.Revision != 8 || restoredOperation.UpdatedAt != updatedAt || restoredState.UpdatedAt != stateUpdatedAt {
		t.Fatalf("idempotent restore reconciliation rewrote records: operation=%#v state=%#v", restoredOperation, restoredState)
	}
}

func nativeOperationGuardRow(workflowState string) NativeFallbackOperationModel {
	return NativeFallbackOperationModel{
		Schema: NativeFallbackOperationSchemaV1, OperationID: "native-operation", Revision: 1, ResourceID: "core:inbound:17", InboundDatabaseID: 17,
		PlanID: strings.Repeat("1", 64), PlanDigest: strings.Repeat("1", 64), PlanJSON: []byte(`{}`),
		RuntimeIdentityRevision: strings.Repeat("2", 64), CapabilityResolverRevision: strings.Repeat("3", 64),
		BeforeConfigurationRevision: strings.Repeat("4", 64), ExpectedAfterRevision: strings.Repeat("5", 64), BeforeEffectiveRevision: strings.Repeat("6", 64),
		TargetReferenceJSON: []byte(`{}`), TargetRevision: strings.Repeat("7", 64), ProviderRevision: "provider-one",
		EndpointRevision: strings.Repeat("8", 64), PublishRevision: "publish-one", HealthRevision: strings.Repeat("9", 64), CapacityRevision: strings.Repeat("a", 64),
		WorkflowState: workflowState, HealthFactsJSON: []byte(`{}`), ReasonCodesJSON: []byte(`[]`), RecoveryBundleJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1,
	}
}

func nativeMirrorGuardRow(state string) FallbackTargetLeaseModel {
	return FallbackTargetLeaseModel{
		Schema: NativeFallbackMirrorSchemaV1, LeaseID: "native-reservation-mirror", HolderID: "native-operation",
		OperationID: "native-operation", ResourceID: "core:inbound:17", ProviderReservationID: "provider-reservation",
		ProviderReservationRevision: "provider-revision", ProviderID: "fixture-provider", TargetID: "target-one",
		PublishRevision: "publish-one", ContentDigest: strings.Repeat("b", 64), ApprovedLocalEndpointID: "endpoint-one",
		ProviderHealthRevision: strings.Repeat("c", 64), IssuedAt: 1, RenewedAt: 1, ExpiresAt: 2, State: state, ReasonCodesJSON: []byte(`[]`),
	}
}
