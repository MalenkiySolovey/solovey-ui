package enforcement_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/enforcement"
)

var actionTime = time.Unix(1000, 0).UTC()

func actionFixture() enforcement.EndpointActionInput {
	action := domain.AppliedActionV1{
		Schema: domain.AppliedActionSchemaV1, DecisionID: strings.Repeat("a", 64), PlanDigest: strings.Repeat("b", 64),
		ResourceID: "core:inbound:one", Subject: domain.SignalSubjectV2{Type: "ip", Value: "203.0.113.10"},
		GraphRevision: strings.Repeat("c", 64), EndpointRevision: strings.Repeat("d", 64),
		ResourceRevision: strings.Repeat("e", 64), ConfigurationRevision: strings.Repeat("f", 64),
		RequestedIntent: domain.IntentTemporaryBlock, ResolvedIntent: domain.IntentTemporaryBlock,
		DesiredState: "REQUESTED", SelectedState: "SELECTED", ActualState: "NOT_APPLIED", State: domain.ActionPlanned,
		CreatedAt: actionTime, ExpiresAt: actionTime.Add(time.Hour),
	}
	action.FinalizeID()
	return enforcement.EndpointActionInput{Action: action, SourceClass: "native"}
}

func actionLimits() enforcement.DynamicSetLimits {
	return enforcement.DynamicSetLimits{MaxElements: 4096, DefaultTTLSeconds: 14400, MaxTTLSeconds: 86400}
}

func TestActionAdmissionRejectsInvalidAuthorityAndPreservesExpiry(t *testing.T) {
	for name, mutate := range map[string]func(*enforcement.EndpointActionInput){
		"unclassified source": func(x *enforcement.EndpointActionInput) { x.SourceClass = "unknown" },
		"changed identity":    func(x *enforcement.EndpointActionInput) { x.Action.Subject.Value = "203.0.113.11" },
		"stale scope": func(x *enforcement.EndpointActionInput) {
			x.Action.GraphRevision = strings.Repeat("1", 64)
			x.Action.FinalizeID()
		},
		"unbounded lifetime": func(x *enforcement.EndpointActionInput) {
			x.Action.ExpiresAt = actionTime.Add(25 * time.Hour)
			x.Action.FinalizeID()
		},
		"applied is not new authority": func(x *enforcement.EndpointActionInput) { x.Action.State = domain.ActionApplied },
		"decoy has no firewall primitive": func(x *enforcement.EndpointActionInput) {
			x.Action.ResolvedIntent = domain.IntentRouteToDecoy
			x.Action.FinalizeID()
		},
		"unknown action": func(x *enforcement.EndpointActionInput) {
			x.Action.ResolvedIntent = "FUTURE_ACTION"
			x.Action.FinalizeID()
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := actionFixture()
			mutate(&value)
			got, reasons := enforcement.AdmitActions([]enforcement.EndpointActionInput{value}, strings.Repeat("c", 64), actionTime, actionLimits())
			if len(got) != 0 || !reflect.DeepEqual(reasons, []string{"action_contract_invalid"}) {
				t.Fatalf("unsafe contribution admitted: %+v, %v", got, reasons)
			}
		})
	}
	expired := actionFixture()
	expired.Action.CreatedAt = actionTime.Add(-2 * time.Hour)
	expired.Action.ExpiresAt = actionTime
	expired.Action.FinalizeID()
	got, reasons := enforcement.AdmitActions([]enforcement.EndpointActionInput{expired}, expired.Action.GraphRevision, actionTime, actionLimits())
	if len(got) != 0 || len(reasons) != 0 {
		t.Fatal("expired actions must disappear without reviving desired enforcement")
	}
}

func TestCurrentTargetMustMatchEveryActionRevision(t *testing.T) {
	value := actionFixture()
	actions, reasons := enforcement.AdmitActions([]enforcement.EndpointActionInput{value}, value.Action.GraphRevision, actionTime, actionLimits())
	if len(reasons) != 0 || len(actions[value.Action.EndpointRevision]) != 1 {
		t.Fatalf("valid action rejected: %v", reasons)
	}
	target := enforcement.Target{ResourceID: value.Action.ResourceID, ActionScopeRevision: value.Action.GraphRevision,
		EndpointRevision: value.Action.EndpointRevision, ResourceRevision: value.Action.ResourceRevision, ConfigurationRevision: value.Action.ConfigurationRevision}
	for name, mutate := range map[string]func(*enforcement.Target){
		"foreign resource":    func(x *enforcement.Target) { x.ResourceID = "core:inbound:other" },
		"stale scope":         func(x *enforcement.Target) { x.ActionScopeRevision = strings.Repeat("1", 64) },
		"wrong endpoint":      func(x *enforcement.Target) { x.EndpointRevision = strings.Repeat("2", 64) },
		"stale resource":      func(x *enforcement.Target) { x.ResourceRevision = strings.Repeat("3", 64) },
		"stale configuration": func(x *enforcement.Target) { x.ConfigurationRevision = strings.Repeat("4", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := target
			mutate(&bad)
			bound, reasons := enforcement.BindActions(bad, actions[value.Action.EndpointRevision])
			if len(bound) != 0 || !reflect.DeepEqual(reasons, []string{"action_revision_stale"}) {
				t.Fatalf("foreign or stale target admitted: %+v %v", bound, reasons)
			}
		})
	}
}

func TestEquivalentNativeAndExternalActionsShareOneBoundedContribution(t *testing.T) {
	first := actionFixture()
	second := first
	second.SourceClass = "external"
	second.Action.DecisionID = strings.Repeat("2", 64)
	second.Action.ExpiresAt = actionTime.Add(30 * time.Minute)
	second.Action.FinalizeID()
	input := []enforcement.EndpointActionInput{first, second, first}
	before := append([]enforcement.EndpointActionInput(nil), input...)
	admitted, reasons := enforcement.AdmitActions(input, first.Action.GraphRevision, actionTime, actionLimits())
	target := enforcement.Target{ResourceID: first.Action.ResourceID, ActionScopeRevision: first.Action.GraphRevision,
		EndpointRevision: first.Action.EndpointRevision, ResourceRevision: first.Action.ResourceRevision, ConfigurationRevision: first.Action.ConfigurationRevision}
	got, bindingReasons := enforcement.BindActions(target, admitted[first.Action.EndpointRevision])
	if len(reasons) != 0 || len(bindingReasons) != 0 || len(got) != 1 || got[0].RefCount != 2 ||
		got[0].Subject != "203.0.113.10/32" || got[0].ExpiresAt != first.Action.ExpiresAt.Unix() || got[0].TTLSeconds != 3600 ||
		// Preserve the released projection, including its repeated first source
		// class. Normalizing that representation is a separate data change.
		!reflect.DeepEqual(got[0].SourceClasses, []string{"external", "native", "native"}) || !reflect.DeepEqual(input, before) {
		t.Fatalf("coalescing changed policy, lifetime or inputs: %+v %v %v", got, reasons, bindingReasons)
	}
	admitted, _ = enforcement.AdmitActions([]enforcement.EndpointActionInput{second, first}, first.Action.GraphRevision, actionTime, actionLimits())
	reordered, _ := enforcement.BindActions(target, admitted[first.Action.EndpointRevision])
	if !reflect.DeepEqual(got, reordered) {
		t.Fatal("provider order changed the desired contribution")
	}
}
