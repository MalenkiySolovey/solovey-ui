package domain

import (
	"strings"
	"testing"
	"time"
)

func TestGraylistStateAndPlannedResponseCannotFalseClaimApplied(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	state := GraylistStateV2{
		Schema: GraylistStateSchemaV2, Revision: 1,
		Subject:    SignalSubjectV2{Type: "ip", Value: "192.0.2.10"},
		ResourceID: "core:inbound:one", EndpointID: "endpoint:tcp:443", Transport: "tcp",
		Score: 20, ConfidenceBP: 9000, PolicyRevision: "policy:two",
		StrategyRevision: "strategy:two", CapabilityRevision: "capability:two",
		SignalRefs: []string{strings.Repeat("a", 64)}, SourceClasses: []string{"native"},
		Band: GraylistBandGraylist, Lifecycle: GraylistLifecycleActive,
		EnteredAt: now, LastSignalAt: now, ExpiresAt: now.Add(time.Hour),
		SelectedResponse: IntentObserve, DesiredAction: IntentSoftGraylist,
		ActualActionState: "NOT_APPLIED", ReasonCodes: []string{},
		CreatedAt: now, UpdatedAt: now,
	}
	state.FinalizeID()
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	state.ActualActionState = "APPLIED"
	if err := state.Validate(); err == nil {
		t.Fatal("graylist state claimed APPLIED without exact executor evidence")
	}
	response := PlannedResponseV2{
		Schema: PlannedResponseSchemaV2, DecisionID: strings.Repeat("b", 64),
		ResourceID: "core:inbound:one", EndpointID: "endpoint:tcp:443",
		Subject:       SignalSubjectV2{Type: "ip", Value: "192.0.2.10"},
		DesiredIntent: IntentRouteToDecoy, SelectedIntent: IntentObserve,
		CapabilityRevision: "capability:two", PolicyRevision: "policy:two", StrategyRevision: "strategy:two",
		ActionScopeRevision: strings.Repeat("c", 64), EndpointRevision: strings.Repeat("d", 64),
		ResourceRevision: strings.Repeat("e", 64), ConfigurationRevision: strings.Repeat("f", 64),
		ActualState: "NOT_APPLIED",
		ReasonCodes: []string{"decision_not_applied"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	response.FinalizeID()
	if err := response.Validate(); err != nil {
		t.Fatal(err)
	}
	response.ActualState = "APPLIED"
	if err := response.Validate(); err == nil {
		t.Fatal("planned response represented itself as APPLIED")
	}
}
