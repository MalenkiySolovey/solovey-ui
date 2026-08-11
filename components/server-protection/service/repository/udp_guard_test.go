package repository

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestUDPGuardPersistenceRestoreAndDropSafety(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	state := UDPGuardStateV1Model{ResourceID: "core:inbound:1", EndpointID: "claim:1", Schema: "solovey-ui/udp-direct-status/v1", DesiredPolicy: "UDP_DIRECT_GUARDED", SelectedStrategy: "UDP_DIRECT_GUARDED", ActualState: "PREPARED", PlanID: "udp-plan:one", PlanDigest: string64("a"), CapabilityRevision: string64("b"), ClaimRevision: string64("c"), PolicyRevision: string64("d"), LatestOperationID: "operation:1", LatestOperationRevision: 1, RecoverableArtifact: true}
	if err := repository.SaveUDPGuardState(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDropSafe(db); err == nil {
		t.Fatal("drop accepted prepared UDP authority")
	}
	if err := ReconcileRestoredUDPGuardRecords(context.Background(), db, time.Unix(2000, 0)); err != nil {
		t.Fatal(err)
	}
	values, err := repository.UDPGuardStates(context.Background())
	if err != nil || len(values) != 1 || values[0].ActualState != "RECOVERY_REQUIRED" || !values[0].RecoveryRequired || values[0].OwnsActiveContribution {
		t.Fatalf("restored=%#v err=%v", values, err)
	}
	if err := db.Model(&UDPGuardStateV1Model{}).Where("id = ?", values[0].ID).Updates(map[string]any{"actual_state": "NOT_APPLIED", "recovery_required": false, "recoverable_artifact": false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureDropSafe(db); err != nil {
		t.Fatalf("terminal UDP state blocked drop: %v", err)
	}
}

func TestUDPGuardBackupSchemaContainsSemanticTablesOnly(t *testing.T) {
	names := map[string]bool{}
	for _, table := range BackupTableModels() {
		names[table.Name] = true
	}
	if !names["server_protection_udp_guard_states_v1"] || !names["server_protection_udp_guard_idempotency_v1"] {
		t.Fatalf("backup tables=%#v", names)
	}
}

func TestUDPGuardIdempotencyIsExactAndAmbiguitySafe(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	ctx := context.Background()
	digest := string64("a")
	receipt, replay, err := repository.BeginUDPGuardReceipt(ctx, "apply", "same-key", digest)
	if err != nil || replay {
		t.Fatalf("begin=%#v replay=%t err=%v", receipt, replay, err)
	}
	if _, _, err := repository.BeginUDPGuardReceipt(ctx, "apply", "same-key", digest); err == nil {
		t.Fatal("pending receipt replayed")
	}
	response := map[string]any{"actualState": "APPLIED_EXPERIMENTAL", "operationId": "operation:1"}
	if err := repository.CompleteUDPGuardReceipt(ctx, receipt.ID, "operation:1", 2, response); err != nil {
		t.Fatal(err)
	}
	again, replay, err := repository.BeginUDPGuardReceipt(ctx, "apply", "same-key", digest)
	if err != nil || !replay || again.OperationRevision != 2 {
		t.Fatalf("replay=%#v %t %v", again, replay, err)
	}
	if _, _, err := repository.BeginUDPGuardReceipt(ctx, "apply", "same-key", string64("b")); err == nil {
		t.Fatal("mismatched idempotency digest accepted")
	}
}

func TestSavingNewActiveSocketBindingDemotesOnlyPreviousSameContributionState(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	base := UDPGuardStateV1Model{Schema: "solovey-ui/udp-status/v1", DesiredPolicy: "UDP_DIRECT_GUARDED", SelectedStrategy: "UDP_DIRECT_GUARDED", ActualState: "APPLIED_EXPERIMENTAL", PlanID: "plan", PlanDigest: strings.Repeat("a", 64), CapabilityRevision: strings.Repeat("b", 64), ClaimRevision: strings.Repeat("c", 64), PolicyRevision: strings.Repeat("d", 64), ContributionRevision: strings.Repeat("e", 64), OwnsActiveContribution: true, RecoverableArtifact: true}
	old := base
	old.ResourceID, old.EndpointID, old.AddressFamily, old.ContributionID = "resource", "old-v4", "ipv4", "udp:v4"
	other := base
	other.ResourceID, other.EndpointID, other.AddressFamily, other.ContributionID = "resource", "current-v6", "ipv6", "udp:v6"
	if err := repository.SaveUDPGuardState(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveUDPGuardState(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	current := base
	current.ResourceID, current.EndpointID, current.AddressFamily, current.ContributionID = "resource", "current-v4", "ipv4", "udp:v4"
	if err := repository.SaveUDPGuardState(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	var rows []UDPGuardStateV1Model
	if err := db.Order("endpoint_id ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	byEndpoint := map[string]UDPGuardStateV1Model{}
	for _, row := range rows {
		byEndpoint[row.EndpointID] = row
	}
	if row := byEndpoint["old-v4"]; row.ActualState != "NOT_APPLIED" || row.OwnsActiveContribution || row.RecoverableArtifact || row.RecoveryRequired {
		t.Fatalf("previous same-contribution state retained authority: %#v", row)
	}
	if row := byEndpoint["current-v4"]; !row.OwnsActiveContribution || !row.RecoverableArtifact {
		t.Fatalf("new active state was not stored: %#v", row)
	}
	if row := byEndpoint["current-v6"]; !row.OwnsActiveContribution || !row.RecoverableArtifact {
		t.Fatalf("independent family state was demoted: %#v", row)
	}
}

func string64(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}
