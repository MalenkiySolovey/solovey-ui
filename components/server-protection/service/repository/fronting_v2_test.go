package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFrontingV2TablesAreOwnedBackedUpAndContainNoRawExecutionFields(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"server_protection_fronting_states_v2", "server_protection_fronting_idempotency_v2"} {
		if !db.Migrator().HasTable(name) {
			t.Fatalf("missing table %s", name)
		}
		found := false
		for _, table := range BackupTableModels() {
			found = found || table.Name == name
		}
		if !found {
			t.Fatalf("backup excludes semantic table %s", name)
		}
	}
	typeOf := reflect.TypeOf(FrontingStateV2Model{})
	for index := 0; index < typeOf.NumField(); index++ {
		name := strings.ToLower(typeOf.Field(index).Name)
		for _, forbidden := range []string{"path", "configbytes", "helperoutput", "argv", "environment", "credential", "secret", "backendhost", "backendport", "directive"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("unsafe persistence field %s", typeOf.Field(index).Name)
			}
		}
	}
}

func TestLegacyFrontingMigrationRequiresRepreviewAndIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	operation := OperationLockModel{OperationID: "legacy-fronting", Kind: "fronting", ResourceID: "core:inbound:legacy", State: "applied", Revision: 7, LockedByInstanceID: "legacy", Actor: "admin", HeartbeatAt: 1, ExpiresAt: 2, CreatedAt: 3, UpdatedAt: 4}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var state FrontingStateV2Model
	if err := db.First(&state, "resource_id = ?", operation.ResourceID).Error; err != nil {
		t.Fatal(err)
	}
	if state.DesiredStrategy != "DISABLED_REPREVIEW_REQUIRED" || state.ActualState != "NOT_APPLIED" || state.CompatibilityState != FrontingCompatibilityLegacyRepreview || state.GuardingProviderLease || state.OwnsActiveManagedRevision {
		t.Fatalf("unsafe legacy projection=%#v", state)
	}
	updated := state.UpdatedAt
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&state, "resource_id = ?", operation.ResourceID).Error; err != nil {
		t.Fatal(err)
	}
	if state.UpdatedAt != updated {
		t.Fatalf("idempotent migration rewrote timestamp %d -> %d", updated, state.UpdatedAt)
	}
}

func TestRestoreDistrustsFrontingActiveStateAndPendingReceiptIdempotently(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	state := frontingTestState("APPLIED")
	state.OwnsActiveManagedRevision, state.GuardingProviderLease, state.RecoverableArtifact = true, true, true
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	receipt := FrontingIdempotencyV2Model{Action: "apply", IdempotencyKey: "restore-pending", RequestDigest: strings.Repeat("a", 64), Status: FrontingReceiptPending, ResponseJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0).UTC()
	if err := ReconcileRestoredFrontingRecords(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&state, "resource_id = ?", state.ResourceID).Error; err != nil {
		t.Fatal(err)
	}
	if state.ActualState != "RECONCILE_REQUIRED" || state.RecoveryClassification != "RESTORED_STATE_UNVERIFIED" || state.OwnsActiveManagedRevision {
		t.Fatalf("restored state=%#v", state)
	}
	if err := db.First(&receipt, "action = ? AND idempotency_key = ?", "apply", "restore-pending").Error; err != nil {
		t.Fatal(err)
	}
	if receipt.Status != FrontingReceiptAmbiguous {
		t.Fatalf("receipt=%#v", receipt)
	}
	if err := ReconcileRestoredFrontingRecords(context.Background(), db, time.Unix(200, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	var again FrontingStateV2Model
	if err := db.First(&again, "resource_id = ?", state.ResourceID).Error; err != nil {
		t.Fatal(err)
	}
	if again.UpdatedAt != state.UpdatedAt {
		t.Fatalf("restore rewrote stable timestamp %d -> %d", state.UpdatedAt, again.UpdatedAt)
	}
}

func TestInvalidFrontingRowsAreRejectedReconciledAndGuardDrop(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	valid := frontingTestState("ROLLED_BACK")
	if err := repository.ProjectFrontingStateV2(context.Background(), valid); err != nil {
		t.Fatalf("valid projection rejected: %v", err)
	}
	invalid := frontingTestState("ROLLED_BACK")
	invalid.ResourceID, invalid.Schema = "fixture:invalid-fronting", "broken-schema"
	if err := repository.ProjectFrontingStateV2(context.Background(), invalid); err == nil {
		t.Fatal("invalid projection accepted")
	}
	if err := db.Create(&invalid).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureDropSafe(db); err == nil {
		t.Fatal("drop accepted an invalid semantic row")
	}
	now := time.Unix(300, 0).UTC()
	if err := ReconcileRestoredFrontingRecords(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&invalid, "resource_id = ?", invalid.ResourceID).Error; err != nil {
		t.Fatal(err)
	}
	if invalid.ActualState != "RECONCILE_REQUIRED" || invalid.RecoveryClassification != "RESTORED_STATE_INVALID" || invalid.UpdatedAt != now.Unix() {
		t.Fatalf("invalid restore projection=%#v", invalid)
	}
	if err := ReconcileRestoredFrontingRecords(context.Background(), db, time.Unix(400, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	var again FrontingStateV2Model
	if err := db.First(&again, "resource_id = ?", invalid.ResourceID).Error; err != nil {
		t.Fatal(err)
	}
	if again.UpdatedAt != invalid.UpdatedAt {
		t.Fatalf("invalid restore rewrote stable timestamp %d -> %d", invalid.UpdatedAt, again.UpdatedAt)
	}
}

func TestFrontingDropGuardsEveryRecoveryAuthorityAndAllowsTerminalCleanup(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*FrontingStateV2Model)
	}{
		{"prepared", func(value *FrontingStateV2Model) { value.ActualState = "PREPARED" }},
		{"applied", func(value *FrontingStateV2Model) { value.ActualState = "APPLIED" }},
		{"degraded", func(value *FrontingStateV2Model) { value.ActualState = "DEGRADED" }},
		{"rollback_failed", func(value *FrontingStateV2Model) { value.ActualState = "ROLLBACK_FAILED" }},
		{"reconcile_required", func(value *FrontingStateV2Model) { value.ActualState = "RECONCILE_REQUIRED" }},
		{"provider_lease", func(value *FrontingStateV2Model) { value.GuardingProviderLease = true }},
		{"artifact", func(value *FrontingStateV2Model) { value.RecoverableArtifact = true }},
		{"active_revision", func(value *FrontingStateV2Model) { value.OwnsActiveManagedRevision = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openTestDB(t)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			state := frontingTestState("NOT_APPLIED")
			test.mutate(&state)
			if err := db.Create(&state).Error; err != nil {
				t.Fatal(err)
			}
			if err := EnsureDropSafe(db); err == nil {
				t.Fatal("drop accepted recovery authority")
			}
		})
	}
	for _, receiptState := range []string{FrontingReceiptPending, FrontingReceiptAmbiguous} {
		t.Run("receipt_"+receiptState, func(t *testing.T) {
			db := openTestDB(t)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			value := FrontingIdempotencyV2Model{Action: "apply", IdempotencyKey: receiptState, RequestDigest: strings.Repeat("a", 64), Status: receiptState, ResponseJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1}
			if err := db.Create(&value).Error; err != nil {
				t.Fatal(err)
			}
			if err := EnsureDropSafe(db); err == nil {
				t.Fatal("drop accepted ambiguous receipt")
			}
		})
	}
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	terminal := frontingTestState("ROLLED_BACK")
	if err := db.Create(&terminal).Error; err != nil {
		t.Fatal(err)
	}
	complete := FrontingIdempotencyV2Model{Action: "rollback", IdempotencyKey: "complete", RequestDigest: strings.Repeat("b", 64), OperationID: "terminal-operation", OperationRevision: 1, Status: FrontingReceiptComplete, ResponseJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&complete).Error; err != nil {
		t.Fatal(err)
	}
	if err := DropSchema(db); err != nil {
		t.Fatalf("terminal cleanup blocked: %v", err)
	}
}

func frontingTestState(actual string) FrontingStateV2Model {
	return FrontingStateV2Model{ResourceID: "fixture:fronting", Schema: FrontingStateSchemaV2, DesiredStrategy: "DISABLED", ActualState: actual,
		ApplyGate: "EXPERIMENTAL_DISABLED_BY_DEFAULT", RuntimeState: "UNKNOWN", InstallationClass: "UNKNOWN", SocketClaimJSON: []byte(`{}`),
		BackendReferencesJSON: []byte(`[]`), FallbackReferencesJSON: []byte(`[]`), SelectorSetJSON: []byte(`{}`), LeaseMirrorsJSON: []byte(`[]`),
		CompatibilityState: FrontingCompatibilityCurrentV2, ReasonCodesJSON: []byte(`[]`), BlocksJSON: []byte(`[]`), WarningsJSON: []byte(`[]`),
		SafeNextAction: "PREVIEW", CreatedAt: 1, UpdatedAt: 1}
}
