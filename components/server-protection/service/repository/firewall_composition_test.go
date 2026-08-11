package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFirewallAuthorityCommitChangesOnlyTransitionContribution(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	baseline := FirewallContributionModel{ContributionID: "managed-firewall:baseline", Schema: "fixture", Kind: "BASELINE", ResourceID: "managed-table", Network: "inet", AddressFamily: "inet", SemanticRevision: strings.Repeat("a", 64), SemanticJSON: json.RawMessage(`{"baseline":true}`), AppliedOperationID: "base", CreatedAt: 1, UpdatedAt: 1}
	udpA := FirewallContributionModel{ContributionID: "udp:a", Schema: "fixture", Kind: "UDP_DIRECT_GUARDED", ResourceID: "resource:a", EndpointID: "endpoint:a", Network: "udp", AddressFamily: "ipv4", SemanticRevision: strings.Repeat("b", 64), SemanticJSON: json.RawMessage(`{"udp":"a"}`), AppliedOperationID: "udp-a", CreatedAt: 1, UpdatedAt: 1}
	udpB := FirewallContributionModel{ContributionID: "udp:b", Schema: "fixture", Kind: "UDP_DIRECT_GUARDED", ResourceID: "resource:b", EndpointID: "endpoint:b", Network: "udp", AddressFamily: "ipv6", SemanticRevision: strings.Repeat("c", 64), SemanticJSON: json.RawMessage(`{"udp":"b"}`), AppliedOperationID: "udp-b", CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&[]FirewallContributionModel{baseline, udpA, udpB}).Error; err != nil {
		t.Fatal(err)
	}
	current := FirewallCompositionModel{ID: 1, Schema: "fixture", Revision: strings.Repeat("d", 64), ManagedPlanRevision: strings.Repeat("e", 64), CandidateSHA256: strings.Repeat("f", 64), BindingsJSON: json.RawMessage(`[]`), State: "ACTIVE", AppliedOperationID: "udp-b", UpdatedAt: 1}
	if err := db.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	transition := FirewallContributionTransitionModel{OperationID: "rollback-a", Schema: "fixture", ContributionID: udpA.ContributionID, PreviousJSON: json.RawMessage(`{}`), DesiredSemanticRevision: udpA.SemanticRevision, DesiredJSON: udpA.SemanticJSON,
		BeforeCompositionRevision: current.Revision, AfterCompositionRevision: current.Revision, ManagedPlanRevision: current.ManagedPlanRevision, CandidateSHA256: current.CandidateSHA256, State: "APPLIED", MarkerUnixNano: 1, MutationCompletedUnixNano: 2, CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&transition).Error; err != nil {
		t.Fatal(err)
	}
	replacementComposition := FirewallCompositionModel{Schema: "fixture", Revision: strings.Repeat("1", 64), ManagedPlanRevision: strings.Repeat("2", 64), CandidateSHA256: strings.Repeat("3", 64), BindingsJSON: json.RawMessage(`[]`)}
	if err := repository.CommitFirewallAuthority(context.Background(), transition.OperationID, current.Revision, udpA.SemanticRevision, nil, replacementComposition, "ROLLED_BACK"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.FirewallAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Contributions) != 2 || snapshot.Contributions[0].ContributionID != baseline.ContributionID || snapshot.Contributions[1].ContributionID != udpB.ContributionID || snapshot.Composition.Revision != replacementComposition.Revision {
		t.Fatalf("unrelated authority changed: %#v", snapshot)
	}
	if err = repository.CommitFirewallAuthority(context.Background(), transition.OperationID, current.Revision, udpA.SemanticRevision, nil, replacementComposition, "ROLLED_BACK"); err == nil {
		t.Fatal("stale composition/contribution fence was accepted")
	}
}

func TestRestoredFirewallAuthorityIsRecoveryRequiredAndDropProtected(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	contribution := FirewallContributionModel{ContributionID: "udp:restore", Schema: "fixture", Kind: "UDP_DIRECT_GUARDED", ResourceID: "resource", EndpointID: "endpoint", Network: "udp", AddressFamily: "ipv4", SemanticRevision: strings.Repeat("a", 64), SemanticJSON: json.RawMessage(`{"udp":true}`), AppliedOperationID: "operation", CreatedAt: 1, UpdatedAt: 1}
	composition := FirewallCompositionModel{ID: 1, Schema: "fixture", Revision: strings.Repeat("b", 64), ManagedPlanRevision: strings.Repeat("c", 64), CandidateSHA256: strings.Repeat("d", 64), BindingsJSON: json.RawMessage(`[]`), State: "ACTIVE", AppliedOperationID: "operation", UpdatedAt: 1}
	transition := FirewallContributionTransitionModel{OperationID: "operation", Schema: "fixture", ContributionID: contribution.ContributionID, PreviousJSON: json.RawMessage(`{}`), DesiredSemanticRevision: contribution.SemanticRevision, DesiredJSON: contribution.SemanticJSON,
		AfterCompositionRevision: composition.Revision, ManagedPlanRevision: composition.ManagedPlanRevision, CandidateSHA256: composition.CandidateSHA256, State: "HEALTH_VERIFIED", MarkerUnixNano: 1, MutationCompletedUnixNano: 2, CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&contribution).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&composition).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&transition).Error; err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestoredFirewallAuthority(context.Background(), db, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	var restoredComposition FirewallCompositionModel
	var restoredTransition FirewallContributionTransitionModel
	if err := db.First(&restoredComposition, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&restoredTransition, "operation_id = ?", transition.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	if restoredComposition.State != "RECOVERY_REQUIRED" || restoredTransition.State != "RECOVERY_REQUIRED" {
		t.Fatalf("restore fabricated active authority: composition=%#v transition=%#v", restoredComposition, restoredTransition)
	}
	if err := DropSchema(db); err == nil {
		t.Fatal("DropSchema deleted restored guarding authority")
	}
	names := map[string]bool{}
	for _, table := range BackupTableModels() {
		names[table.Name] = true
	}
	for _, name := range []string{contribution.TableName(), composition.TableName(), transition.TableName()} {
		if !names[name] {
			t.Fatalf("backup omits firewall authority table %s", name)
		}
	}
}
