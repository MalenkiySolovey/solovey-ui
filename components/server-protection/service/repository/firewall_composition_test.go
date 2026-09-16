package repository

import (
	"context"
	"encoding/json"
	"errors"
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
	replacementComposition := FirewallCompositionModel{Schema: "fixture", Revision: strings.Repeat("1", 64), ManagedPlanRevision: strings.Repeat("2", 64), CandidateSHA256: strings.Repeat("3", 64), CandidateSemanticSHA256: strings.Repeat("4", 64), BindingsJSON: json.RawMessage(`[]`)}
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
	if !snapshot.HasObservation || snapshot.Observation.State != "MATCHING" || snapshot.Observation.CommittedCompositionRevision != replacementComposition.Revision || snapshot.Observation.CurrentSemanticSHA256 != replacementComposition.CandidateSemanticSHA256 {
		t.Fatalf("commit did not publish the post-mutation live observation: %#v", snapshot.Observation)
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

func TestRestoreClosesAbandonedFirewallPreparationsWithoutHidingMutationAuthority(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"abandoned", "already_normalized", "mutation_marked", "live_reference", "still_prepared"} {
		state := "abandoned"
		if id == "still_prepared" {
			state = "prepared"
		}
		if err := db.Create(&OperationLockModel{OperationID: id, IdempotencyKey: id, Kind: "firewall", State: state, Revision: 1}).Error; err != nil {
			t.Fatal(err)
		}
		transition := FirewallContributionTransitionModel{OperationID: id, State: "PREPARED", UpdatedAt: 1,
			PreviousJSON: json.RawMessage(`{}`), DesiredJSON: json.RawMessage(`{}`)}
		if id == "already_normalized" {
			transition.State = "RECOVERY_REQUIRED"
		}
		if id == "mutation_marked" {
			transition.MarkerUnixNano = 1
		}
		if err := db.Create(&transition).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&FirewallContributionModel{ContributionID: "retained", AppliedOperationID: "live_reference", SemanticJSON: json.RawMessage(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestoredFirewallAuthority(t.Context(), db, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"abandoned", "already_normalized", "mutation_marked", "live_reference", "still_prepared"} {
		var transition FirewallContributionTransitionModel
		if err := db.First(&transition, "operation_id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		want := "RECOVERY_REQUIRED"
		if id == "abandoned" || id == "already_normalized" {
			want = "CANCELLED"
		}
		if transition.State != want {
			t.Fatalf("%s state=%s want=%s", id, transition.State, want)
		}
	}
	protected, err := protectedArtifactOperations(db)
	if err != nil {
		t.Fatal(err)
	}
	if protected["abandoned"] != "" || protected["already_normalized"] != "" || protected["mutation_marked"] == "" || protected["live_reference"] == "" {
		t.Fatalf("restore retention closure=%v", protected)
	}
	// The startup path also repairs previously normalized r24 history and is
	// idempotent; terminalization never rewrites an already closed row.
	if err := db.Model(&FirewallContributionTransitionModel{}).Where("operation_id = ?", "already_normalized").Update("state", "RECOVERY_REQUIRED").Error; err != nil {
		t.Fatal(err)
	}
	if err := ReconcileAbandonedFirewallTransitions(t.Context(), db, time.Unix(2000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileAbandonedFirewallTransitions(t.Context(), db, time.Unix(3000, 0)); err != nil {
		t.Fatal(err)
	}
	var terminal FirewallContributionTransitionModel
	if err := db.First(&terminal, "operation_id = ?", "already_normalized").Error; err != nil {
		t.Fatal(err)
	}
	if terminal.State != "CANCELLED" || terminal.UpdatedAt != time.Unix(2000, 0).UnixNano() {
		t.Fatal("startup normalization is not idempotent")
	}
}

func TestFirewallObservationIsHostLocalAndCompositionFenced(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	if err := repository.RecordFirewallObservation(t.Context(), FirewallObservationModel{State: "ABSENT"}, ""); err != nil {
		t.Fatalf("record inactive/absent: %v", err)
	}
	snapshot, err := repository.FirewallAuthority(t.Context())
	if err != nil || snapshot.HasComposition || !snapshot.HasObservation || snapshot.Observation.State != "ABSENT" || snapshot.Observation.HasCommittedAuthority || snapshot.Observation.ObservedAt <= 0 {
		t.Fatalf("inactive live observation is not durable: snapshot=%#v err=%v", snapshot, err)
	}

	composition := FirewallCompositionModel{ID: 1, Schema: "fixture", Revision: strings.Repeat("a", 64), ManagedPlanRevision: strings.Repeat("b", 64), CandidateSHA256: strings.Repeat("c", 64), CandidateSemanticSHA256: strings.Repeat("d", 64), BindingsJSON: json.RawMessage(`[]`), State: "ACTIVE", AppliedOperationID: "operation", UpdatedAt: 1}
	if err := db.Create(&composition).Error; err != nil {
		t.Fatal(err)
	}
	matching := FirewallObservationModel{State: "MATCHING", ManagedTablePresent: true, CurrentRevision: composition.ManagedPlanRevision, CurrentSemanticSHA256: composition.CandidateSemanticSHA256}
	if err := repository.RecordFirewallObservation(t.Context(), matching, strings.Repeat("e", 64)); !errors.Is(err, ErrFirewallAuthorityConflict) {
		t.Fatalf("stale composition revision accepted: %v", err)
	}
	if err := repository.RecordFirewallObservation(t.Context(), matching, composition.Revision); err != nil {
		t.Fatalf("record matching observation: %v", err)
	}
	snapshot, err = repository.FirewallAuthority(t.Context())
	if err != nil || !snapshot.HasObservation || snapshot.Observation.State != "MATCHING" || snapshot.Observation.CommittedCompositionRevision != composition.Revision || snapshot.Composition.State != "ACTIVE" {
		t.Fatalf("live observation overwrote or detached committed authority: snapshot=%#v err=%v", snapshot, err)
	}
	for _, table := range BackupTableModels() {
		if table.Name == (FirewallObservationModel{}).TableName() {
			t.Fatal("host-local live observation entered portable backup authority")
		}
	}
}

func TestRetireFirewallAuthorityAfterRuntimeLossIsExactAndAtomic(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	operation := OperationLockModel{OperationID: "runtime-loss", Kind: "firewall", State: "applied", Revision: 7, LockedByInstanceID: "old-process", Actor: "admin", HeartbeatAt: 1, ExpiresAt: 2, CreatedAt: 1, UpdatedAt: 1}
	contribution := FirewallContributionModel{ContributionID: "managed-firewall:baseline", Schema: "fixture", Kind: "BASELINE", ResourceID: "managed-table", Network: "inet", AddressFamily: "inet", SemanticRevision: strings.Repeat("a", 64), SemanticJSON: json.RawMessage(`{"baseline":true}`), AppliedOperationID: operation.OperationID, CreatedAt: 1, UpdatedAt: 1}
	composition := FirewallCompositionModel{ID: 1, Schema: "fixture", Revision: strings.Repeat("b", 64), ManagedPlanRevision: strings.Repeat("c", 64), CandidateSHA256: strings.Repeat("d", 64), CandidateSemanticSHA256: strings.Repeat("e", 64), BindingsJSON: json.RawMessage(`[]`), State: "ACTIVE", AppliedOperationID: operation.OperationID, UpdatedAt: 1}
	transition := FirewallContributionTransitionModel{OperationID: operation.OperationID, Schema: "fixture", ContributionID: contribution.ContributionID, PreviousJSON: json.RawMessage(`{}`), DesiredSemanticRevision: contribution.SemanticRevision, DesiredJSON: contribution.SemanticJSON, AfterCompositionRevision: composition.Revision, ManagedPlanRevision: composition.ManagedPlanRevision, CandidateSHA256: composition.CandidateSHA256, CandidateSemanticSHA256: composition.CandidateSemanticSHA256, State: "HEALTH_VERIFIED", MarkerUnixNano: 1, MutationCompletedUnixNano: 2, CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&contribution).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&composition).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&transition).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordFirewallObservation(t.Context(), FirewallObservationModel{State: "ABSENT"}, composition.Revision); err != nil {
		t.Fatal(err)
	}

	if err := repository.RetireFirewallAuthorityAfterRuntimeLoss(t.Context(), operation.OperationID, operation.Revision+1, composition.Revision); !errors.Is(err, ErrFirewallAuthorityConflict) {
		t.Fatalf("stale operation revision was accepted: %v", err)
	}
	before, err := repository.FirewallAuthority(t.Context())
	if err != nil || !before.HasComposition || len(before.Contributions) != 1 || !before.Observation.HasCommittedAuthority {
		t.Fatalf("failed retirement was not atomic: snapshot=%#v err=%v", before, err)
	}

	if err := repository.RetireFirewallAuthorityAfterRuntimeLoss(t.Context(), operation.OperationID, operation.Revision, composition.Revision); err != nil {
		t.Fatal(err)
	}
	after, err := repository.FirewallAuthority(t.Context())
	if err != nil || after.HasComposition || len(after.Contributions) != 0 || !after.HasObservation || after.Observation.State != "ABSENT" || after.Observation.HasCommittedAuthority || after.Observation.CommittedCompositionRevision != "" {
		t.Fatalf("runtime-loss retirement left mixed durable authority: snapshot=%#v err=%v", after, err)
	}
	var retiredTransition FirewallContributionTransitionModel
	if err := db.First(&retiredTransition, "operation_id = ?", operation.OperationID).Error; err != nil || retiredTransition.State != "RETIRED_RUNTIME_LOSS" {
		t.Fatalf("rollback transition was not retired: transition=%#v err=%v", retiredTransition, err)
	}
	if err := repository.RetireFirewallAuthorityAfterRuntimeLoss(t.Context(), operation.OperationID, operation.Revision, ""); err != nil {
		t.Fatalf("exact repeated retirement was not idempotent: %v", err)
	}
}
