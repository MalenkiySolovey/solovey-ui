package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/graylist"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

func TestGraylistV2MigrationStoreRestoreBackupAndDropOwnership(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	legacy := GraylistModel{ResourceID: "core:inbound:legacy", IPCIDR: "192.0.2.0/24", IPFamily: 4, Score: 90, Reason: "legacy", LastSignal: "fallback_hit", ExpiresAt: now.Add(time.Hour).Unix(), CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var legacyV2 GraylistStateV2Model
	if err := db.Where("lifecycle = ?", string(domain.GraylistLifecycleLegacyStale)).First(&legacyV2).Error; err != nil {
		t.Fatal(err)
	}
	var legacyContract domain.GraylistStateV2
	if json.Unmarshal(legacyV2.ContractJSON, &legacyContract) != nil || legacyContract.ActualActionState != "NOT_APPLIED" ||
		legacyContract.SelectedResponse != domain.IntentObserve || legacyContract.DesiredAction != domain.IntentObserve {
		t.Fatalf("legacy action-like values became current truth: %#v", legacyContract)
	}

	state := repositoryGraylistState(t, now)
	repository := New(db)
	wrote, err := repository.StoreGraylistEvaluation(context.Background(), state, 0)
	if err != nil || !wrote {
		t.Fatalf("initial store wrote=%v err=%v", wrote, err)
	}
	wrote, err = repository.StoreGraylistEvaluation(context.Background(), state, state.Revision)
	if err != nil || wrote {
		t.Fatalf("unchanged state caused a write: wrote=%v err=%v", wrote, err)
	}
	forged := state
	forged.ActualActionState = "APPLIED"
	forged.AppliedActionRefID = strings.Repeat("a", 64)
	forged.Revision++
	if _, err := repository.StoreGraylistEvaluation(context.Background(), forged, state.Revision); err == nil {
		t.Fatal("generic graylist store assigned APPLIED")
	}
	if err := ReconcileRestoredGraylistStates(context.Background(), db, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	restored, err := repository.LoadGraylistStateV2(context.Background(), state.StateID)
	if err != nil || restored.Lifecycle != domain.GraylistLifecycleSuperseded || !containsString(restored.ReasonCodes, "restored_state_untrusted") {
		t.Fatalf("restored state remained trusted: %#v err=%v", restored, err)
	}
	updatedAt := restored.UpdatedAt
	if err := ReconcileRestoredGraylistStates(context.Background(), db, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	again, err := repository.LoadGraylistStateV2(context.Background(), state.StateID)
	if err != nil || !again.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("restore reconciliation was not idempotent: %#v err=%v", again, err)
	}
	names := map[string]bool{}
	for _, table := range BackupTableModels() {
		names[table.Name] = true
	}
	if !names["server_protection_graylist_v2"] {
		t.Fatal("graylist V2 table is absent from backup ownership")
	}
	if err := DropSchema(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable(&GraylistStateV2Model{}) {
		t.Fatal("DropData ownership left graylist V2 behind")
	}
}

func TestSignalReplayIsIdempotentAndConflictingReplayFailsClosed(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	signal := repositorySignal(now)
	repository := New(db)
	if err := repository.SaveSignalV2(context.Background(), signal); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveSignalV2(context.Background(), signal); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	conflict := signal
	conflict.ConfidenceBP--
	if err := repository.SaveSignalV2(context.Background(), conflict); err == nil {
		t.Fatal("conflicting replay was silently ignored")
	}
}

func TestSignalPrivacyIsRejectedBeforeInsert(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	signal := repositorySignal(time.Now().UTC().Truncate(time.Second))
	signal.SafeMeta = map[string]string{"pcap": "alphanumericvalue"}
	signal.FinalizeID("privacy:pcap")
	if err := New(db).SaveSignalV2(context.Background(), signal); err == nil {
		t.Fatal("pcap metadata reached signal persistence")
	}
	var count int64
	if err := db.Model(&ProtectionSignalV2Model{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected privacy signal rows=%d err=%v", count, err)
	}
}

func TestAdmittedPipelineTransactionIsReplaySafeAndRollsBackOutOfOrder(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	repository := New(db)
	signal := repositorySignal(now)
	input := repositoryPipelineInput(signal, now)
	accepted := graylist.AcceptedSignal{Signal: signal, Delta: 2, EvidenceClass: domain.GraylistEvidenceStrongTrusted}
	first, err := repository.ProcessAdmittedSignalV2(context.Background(), input, accepted)
	if err != nil {
		t.Fatal(err)
	}
	if first.State.Score != 2 || first.Resolution.PlannedResponse == nil || first.Resolution.PlannedResponse.ActualState != "NOT_APPLIED" {
		t.Fatalf("first atomic pipeline result=%#v", first)
	}
	replay, err := repository.ProcessAdmittedSignalV2(context.Background(), input, accepted)
	if err != nil || replay.State.Score != first.State.Score || replay.Changed {
		t.Fatalf("replay changed score/state: %#v err=%v", replay, err)
	}
	var signals, states, decisions, plans int64
	for model, target := range map[any]*int64{
		&ProtectionSignalV2Model{}: &signals, &GraylistStateV2Model{}: &states,
		&ProtectionDecisionV2Model{}: &decisions, &PlannedResponseV2Model{}: &plans,
	} {
		if err := db.Model(model).Count(target).Error; err != nil {
			t.Fatal(err)
		}
	}
	if signals != 1 || states != 1 || decisions != 1 || plans != 1 {
		t.Fatalf("atomic rows signal=%d state=%d decision=%d plan=%d", signals, states, decisions, plans)
	}

	older := repositorySignal(now.Add(-time.Minute))
	older.FinalizeID("event:older")
	olderInput := repositoryPipelineInput(older, now.Add(time.Second))
	olderAccepted := graylist.AcceptedSignal{Signal: older, Delta: 2, EvidenceClass: domain.GraylistEvidenceStrongTrusted}
	if _, err := repository.ProcessAdmittedSignalV2(context.Background(), olderInput, olderAccepted); !errors.Is(err, graylist.ErrSignalOutOfOrder) {
		t.Fatalf("out-of-order error=%v", err)
	}
	if err := db.Model(&ProtectionSignalV2Model{}).Count(&signals).Error; err != nil || signals != 1 {
		t.Fatalf("out-of-order transaction left signal rows=%d err=%v", signals, err)
	}
	for model, expected := range map[any]int64{
		&GraylistStateV2Model{}: 1, &ProtectionDecisionV2Model{}: 1, &PlannedResponseV2Model{}: 1,
	} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != expected {
			t.Fatalf("out-of-order transaction changed %T rows=%d err=%v", model, count, err)
		}
	}
	var persisted GraylistStateV2Model
	if err := db.Where("state_id = ?", first.State.StateID).First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Revision != first.State.Revision || persisted.LastSignalAt != first.State.LastSignalAt.Unix() {
		t.Fatalf("out-of-order transaction changed state revision=%d last_signal_at=%d", persisted.Revision, persisted.LastSignalAt)
	}

	conflict := signal
	conflict.ConfidenceBP--
	conflictAccepted := accepted
	conflictAccepted.Signal = conflict
	if _, err := repository.ProcessAdmittedSignalV2(context.Background(), input, conflictAccepted); err == nil {
		t.Fatal("same-time conflicting signal replay was accepted")
	}
	if err := db.Model(&ProtectionSignalV2Model{}).Count(&signals).Error; err != nil || signals != 1 {
		t.Fatalf("conflicting replay transaction changed signal rows=%d err=%v", signals, err)
	}

	newer := repositorySignal(now.Add(time.Minute))
	newer.FinalizeID("event:newer")
	newerInput := repositoryPipelineInput(newer, now.Add(time.Minute))
	newerAccepted := graylist.AcceptedSignal{Signal: newer, Delta: 2, EvidenceClass: domain.GraylistEvidenceStrongTrusted}
	advanced, err := repository.ProcessAdmittedSignalV2(context.Background(), newerInput, newerAccepted)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.State.Score != 4 || !advanced.State.LastSignalAt.Equal(newer.ObservedAt) || !advanced.Changed {
		t.Fatalf("newer signal did not advance state: %#v", advanced.State)
	}
	if err := db.Model(&ProtectionSignalV2Model{}).Count(&signals).Error; err != nil || signals != 2 {
		t.Fatalf("newer signal rows=%d err=%v", signals, err)
	}
}

func TestLongLivedLegacyGraylistIsPreservedAsStableNonActionableProjection(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	legacy := GraylistModel{ResourceID: "core:inbound:long-lived", IPCIDR: "198.51.100.0/24", IPFamily: 4, Score: 100, Reason: "legacy", LastSignal: "block", ExpiresAt: now.Add(time.Hour).Unix(), CreatedAt: now.Add(-72 * time.Hour).Unix(), UpdatedAt: now.Unix()}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var rows []GraylistStateV2Model
	if err := db.Where("lifecycle = ?", string(domain.GraylistLifecycleLegacyStale)).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var projected domain.GraylistStateV2
	for _, row := range rows {
		var candidate domain.GraylistStateV2
		if json.Unmarshal(row.ContractJSON, &candidate) == nil && candidate.ResourceID == legacy.ResourceID {
			projected = candidate
			break
		}
	}
	if projected.StateID == "" || projected.ActualActionState != "NOT_APPLIED" || projected.DesiredAction != domain.IntentObserve ||
		!containsString(projected.ReasonCodes, "legacy_timestamp_window_normalized") || projected.ExpiresAt.After(projected.CreatedAt.Add(24*time.Hour)) {
		t.Fatalf("long-lived legacy projection=%#v", projected)
	}
	revision, updatedAt := projected.Revision, projected.UpdatedAt
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	again, err := New(db).LoadGraylistStateV2(context.Background(), projected.StateID)
	if err != nil || again.Revision != revision || !again.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("repeated migration changed projection=%#v err=%v", again, err)
	}
}

func repositoryGraylistState(t *testing.T, now time.Time) domain.GraylistStateV2 {
	t.Helper()
	signal := repositorySignal(now)
	accepted := graylist.AcceptedSignal{Signal: signal, Delta: 20}
	policy := graylist.DefaultPolicyV2()
	policy.Revision = signal.Provenance.PolicyRevision
	result, err := graylist.Evaluate(graylist.EvaluateInput{Accepted: &accepted, Policy: policy, StrategyRevision: "strategy-revision-v2", CapabilityRevision: "capability-revision-v2", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return result.State
}

func repositorySignal(now time.Time) domain.ProtectionSignalV2 {
	signal := domain.ProtectionSignalV2{
		Schema:   domain.ProtectionSignalSchemaV2,
		Source:   domain.SignalSourceV2{SourceID: "source:exact", Producer: "producer:exact", ProducerVersion: "v2", TrustClass: "trusted_endpoint", SourceClass: "native"},
		Category: domain.SignalCategoryConnectionMetadata, Kind: string(domain.SignalFallbackHit), KnownKind: true,
		Subject:    domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"},
		Scope:      domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: "core:inbound:one", EndpointID: "endpoint:tcp:443", Transport: "tcp"},
		ObservedAt: now, ExpiresAt: now.Add(time.Hour), ConfidenceBP: 9000,
		Provenance: domain.SignalProvenanceV2{AdapterID: "adapter:exact", SourceRevision: "source-revision-v2", PolicyRevision: "graylist-policy-v2", ObservationWindowID: "window:one"},
	}
	signal.FinalizeID("event:one")
	return signal
}

func repositoryPipelineInput(signal domain.ProtectionSignalV2, now time.Time) graylist.PipelineInput {
	actionScope := strings.Repeat("a", 64)
	endpointRevision := strings.Repeat("b", 64)
	resourceRevision := strings.Repeat("c", 64)
	configurationRevision := strings.Repeat("d", 64)
	capability := domain.StrategyActionCapabilityV2{
		Schema: domain.StrategyActionCapabilitySchemaV2, Strategy: string(protectionresources.StrategyDirectGuarded),
		ActionRevision: actionScope, StrategyRevision: "strategy-revision-v2", CapabilityRevision: "capability-revision-v2",
		ResourceID: signal.Scope.TargetResourceID, EndpointID: signal.Scope.EndpointID, ActionScopeRevision: actionScope,
		EndpointRevision: endpointRevision, ResourceRevision: resourceRevision, ConfigurationRevision: configurationRevision,
		SameScopeRateLimit: true, Provenance: "repository-pipeline-test", ObservedAt: now, ExpiresAt: now.Add(time.Hour),
		ReasonCodes: []string{},
	}
	policy := graylist.PolicyV2{Revision: signal.Provenance.PolicyRevision, EnterScore: 2, ExitScore: 1, RateScore: 4, RateConfidenceBP: 1, MaxScore: 100, DecayInterval: time.Hour, TTL: time.Hour}
	return graylist.PipelineInput{
		Signal: signal, Policy: policy, Strategy: protectionresources.StrategyDirectGuarded,
		StrategyRevision: capability.StrategyRevision, CapabilityRevision: capability.CapabilityRevision, Capability: capability,
		AllowlistResult: domain.PolicyCheckV2{Result: "deny"}, RecoveryResult: domain.PolicyCheckV2{Result: "allow"},
		Guard:               protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true},
		ActionScopeRevision: actionScope, EndpointRevision: endpointRevision, ResourceRevision: resourceRevision,
		ConfigurationRevision: configurationRevision, EndpointKnown: true, Now: now,
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
