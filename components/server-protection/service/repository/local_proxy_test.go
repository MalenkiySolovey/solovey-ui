package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func localProxyStateFixture(actual string) LocalProxyStateV1Model {
	digest := strings.Repeat("a", 64)
	return LocalProxyStateV1Model{
		ResourceID: "core:inbound:17", EndpointID: "tcp:ipv4:1080",
		Schema: "solovey-ui/local-proxy-guard-state/v1", ActualState: actual,
		ApplyGate: "EXPERIMENTAL_ACK_REQUIRED", PlanID: "local-proxy-plan:fixture",
		PlanDigest: digest, PlanJSON: json.RawMessage(`{"schema":"solovey-ui/local-proxy-guard-plan/v1"}`),
		FactRevision: digest, ReferenceRevision: digest, LeaseID: "local-proxy-lease-1",
		LeaseRevision: digest, LeaseState: "ACTIVE", LeaseRenewedAt: 10, LeaseExpiresAt: 20,
		LatestOperationID: "operation-1", LatestOperationRevision: 4, MarkerRevision: digest,
		HealthJSON: json.RawMessage(`[]`), HealthRevision: digest, HealthExpiresUnixNano: 30,
		GuardingProviderLease: true, CreatedAt: 1, UpdatedAt: 1,
	}
}

func TestLocalProxyTablesAreMigratedBackedUpAndSecretFree(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"server_protection_local_proxy_states_v1", "server_protection_local_proxy_idempotency_v1"} {
		if !db.Migrator().HasTable(name) {
			t.Fatalf("missing table %s", name)
		}
		backedUp := false
		for _, table := range BackupTableModels() {
			backedUp = backedUp || table.Name == name
		}
		if !backedUp {
			t.Fatalf("backup excludes %s", name)
		}
	}
	for _, modelType := range []reflect.Type{reflect.TypeOf(LocalProxyStateV1Model{}), reflect.TypeOf(LocalProxyIdempotencyV1Model{})} {
		for index := 0; index < modelType.NumField(); index++ {
			name := strings.ToLower(modelType.Field(index).Name + " " + modelType.Field(index).Tag.Get("gorm"))
			for _, forbidden := range []string{"credential", "username", "password", "authorization", "destination", "rawconfig", "bindaddress", "listenport", "url", "domain"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("unsafe persistence field %s", modelType.Field(index).Name)
				}
			}
		}
	}
}

func TestRestoreDistrustsLocalProxyMirrorAuthorityAndPendingReceipt(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	state := localProxyStateFixture("APPLIED_EXPERIMENTAL")
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	receipt := LocalProxyIdempotencyV1Model{
		Action: "apply", IdempotencyKey: "pending-restore", RequestDigest: strings.Repeat("b", 64),
		Status: "PENDING", SemanticResponseJSON: json.RawMessage(`{}`), CreatedAt: 1, UpdatedAt: 1,
	}
	if err := db.Create(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0).UTC()
	if err := ReconcileRestoredLocalProxyRecords(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&state, state.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.ActualState != "RECOVERY_REQUIRED" || !state.RecoveryRequired || state.GuardingProviderLease {
		t.Fatalf("restored state trusted mirror authority: %#v", state)
	}
	if err := db.First(&receipt, receipt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "AMBIGUOUS" {
		t.Fatalf("pending receipt was trusted after restore: %#v", receipt)
	}
}

func TestLocalProxyDropSafetyAndIdempotencyConflicts(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	state := localProxyStateFixture("PREPARED")
	if err := repository.SaveLocalProxyState(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDropSafe(db); err == nil {
		t.Fatal("drop accepted retained provider authority")
	}
	if err := db.Model(&LocalProxyStateV1Model{}).Where("resource_id = ?", state.ResourceID).
		Updates(map[string]any{"actual_state": "NOT_APPLIED", "guarding_provider_lease": false, "recovery_required": false}).Error; err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("c", 64)
	receipt, replay, err := repository.BeginLocalProxyReceipt(context.Background(), "prepare", "key-1", digest)
	if err != nil || replay {
		t.Fatalf("receipt=%#v replay=%v err=%v", receipt, replay, err)
	}
	if _, _, err := repository.BeginLocalProxyReceipt(context.Background(), "prepare", "key-1", strings.Repeat("d", 64)); err == nil {
		t.Fatal("idempotency key accepted different request")
	}
	if err := EnsureDropSafe(db); err == nil {
		t.Fatal("drop accepted pending receipt")
	}
	if err := repository.CompleteLocalProxyReceipt(context.Background(), receipt.ID, "operation-1", 1, map[string]string{"actualState": "PREPARED"}); err != nil {
		t.Fatal(err)
	}
	completed, replay, err := repository.ReplayLocalProxyReceipt(context.Background(), "prepare", "key-1", digest)
	if err != nil || !replay || completed.OperationID != "operation-1" {
		t.Fatalf("read-only completed receipt replay failed: %#v replay=%v err=%v", completed, replay, err)
	}
	if _, _, err := repository.ReplayLocalProxyReceipt(context.Background(), "prepare", "key-1", strings.Repeat("d", 64)); err == nil {
		t.Fatal("read-only receipt replay accepted a different request")
	}
	complete, replay, err := repository.BeginLocalProxyReceipt(context.Background(), "prepare", "key-1", digest)
	if err != nil || !replay || complete.OperationID != "operation-1" {
		t.Fatalf("completed receipt did not replay: %#v replay=%v err=%v", complete, replay, err)
	}
	if err := EnsureDropSafe(db); err != nil {
		t.Fatalf("safe terminal state could not drop: %v", err)
	}
}
