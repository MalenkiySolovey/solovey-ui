package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestNativeFallbackStateMigrationUniqueOwnershipAndMissingProjection(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	if !db.Migrator().HasTable(&NativeFallbackStateModel{}) {
		t.Fatal("native fallback state table missing")
	}
	missing, err := New(db).NativeFallbackState(context.Background(), "core:inbound:missing")
	if err != nil || missing.ActualState != domain.NativeActualNotApplied || missing.SelectedVariant != domain.NativeFallbackVariantNone {
		t.Fatalf("missing row projection = %#v err=%v", missing, err)
	}
	row := nativeFallbackStateRow(domain.NativeActualNotApplied)
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := row
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate resource state row was accepted")
	}
	owned := false
	for _, table := range TableModels() {
		if table.Name == "server_protection_native_fallback_states" {
			owned = true
		}
	}
	if !owned {
		t.Fatal("state table is not component-owned")
	}
}

func TestNativeFallbackStateProjectionNeverInfersApplied(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, actual := range []domain.NativeFallbackActualState{
		domain.NativeActualPrepared, domain.NativeActualApplying, domain.NativeActualHealth, domain.NativeActualApplied,
		domain.NativeActualDegraded, domain.NativeActualRollingBack, domain.NativeActualRollbackFailed,
	} {
		t.Run(string(actual), func(t *testing.T) {
			row := nativeFallbackStateRow(actual)
			row.ResourceID += ":" + strings.ToLower(string(actual))
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			state, err := New(db).NativeFallbackState(context.Background(), row.ResourceID)
			if err != nil || state.ActualState != domain.NativeActualReconcileRequired {
				t.Fatalf("historical %s projected as %#v err=%v", actual, state, err)
			}
		})
	}
}

func TestNativeFallbackStateInvalidRowsFailClosedAndRestoreIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0).UTC()
	invalid := nativeFallbackStateRow(domain.NativeActualNotApplied)
	invalid.ResourceID = "core:inbound:invalid"
	invalid.ReasonCodesJSON = []byte(`{"not":"an array"}`)
	active := nativeFallbackStateRow(domain.NativeActualApplied)
	active.ResourceID = "core:inbound:restored"
	if err := db.Create(&invalid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	state, err := New(db).NativeFallbackState(context.Background(), invalid.ResourceID)
	if err != nil || state.ActualState != domain.NativeActualReconcileRequired {
		t.Fatalf("invalid row did not fail closed: %#v err=%v", state, err)
	}
	if err := ReconcileRestoredNativeFallbackStates(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestoredNativeFallbackStates(context.Background(), db, now.Add(time.Hour)); err != nil {
		t.Fatalf("idempotent restore reconciliation: %v", err)
	}
	var restored NativeFallbackStateModel
	if err := db.Where("resource_id = ?", active.ResourceID).First(&restored).Error; err != nil {
		t.Fatal(err)
	}
	if restored.ActualState != string(domain.NativeActualReconcileRequired) {
		t.Fatalf("restored APPLIED row remained trusted: %#v", restored)
	}
	if restored.UpdatedAt != now.Unix() {
		t.Fatalf("idempotent reconciliation rewrote updated_at: %d", restored.UpdatedAt)
	}
	var reasons []domain.NativeFallbackReasonCode
	if json.Unmarshal(restored.ReasonCodesJSON, &reasons) != nil || len(reasons) == 0 {
		t.Fatalf("restored reasons invalid: %s", restored.ReasonCodesJSON)
	}
}

func nativeFallbackStateRow(actual domain.NativeFallbackActualState) NativeFallbackStateModel {
	now := time.Unix(1_900_000_000, 0).UTC().Unix()
	reasons, _ := json.Marshal([]domain.NativeFallbackReasonCode{})
	return NativeFallbackStateModel{
		ResourceID: "core:inbound:17", Schema: domain.NativeFallbackStateSchemaV1, InboundDatabaseID: 17,
		LatestPlanID: strings.Repeat("1", 64), LatestPlanDigest: strings.Repeat("1", 64), RuntimeIdentityRevision: strings.Repeat("2", 64),
		CapabilityResolverRevision: strings.Repeat("3", 64), BeforeConfigurationRevision: strings.Repeat("4", 64), AfterConfigurationRevision: strings.Repeat("5", 64),
		EffectiveRevision: strings.Repeat("6", 64), TargetRevision: strings.Repeat("7", 64), ProviderRevision: "provider-one", EndpointRevision: strings.Repeat("8", 64),
		PublishRevision: "publish-one", HealthRevision: strings.Repeat("9", 64), CapacityRevision: strings.Repeat("a", 64),
		DesiredState: string(domain.NativeFallbackDesired), SelectedVariant: string(domain.NativeFallbackVLESSRealityHandshakeTCP), ActualState: string(actual),
		ReasonCodesJSON: reasons, CreatedAt: now, UpdatedAt: now,
	}
}
