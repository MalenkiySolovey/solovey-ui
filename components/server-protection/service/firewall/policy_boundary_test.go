package firewall

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

// These fingerprints bind the complete serialized plan, typed intent and
// managed candidate to the accepted implementation before separating policy.
func TestProtectionPolicyBoundaryPreservesAcceptedSemantics(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	resource := endpointResourceFixture()
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{
		Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{endpointSurfaceFixture(resource, now)}, Now: now,
	})
	want := map[domain.ResponseIntent]string{
		domain.IntentSoftGraylist:        "8044a371bb8c4a21d93d1a44aa4d3503ed106fd1dfb6879864e84e72b708deee",
		domain.IntentRateLimit:           "0e1e64064d79f0ba4d602decb8100933ae111b2d9e8562d512f83ed12c2f1b90",
		domain.IntentTemporaryQuarantine: "c9c1aba651c78402ecb1d6e70eecb78ba542e0bf115fe7b4fee677730944add0",
		domain.IntentTemporaryBlock:      "8c344f87148532da425504f74e2cc1452e5996853a4525989a9d020312ddaba5",
	}
	for _, intent := range []domain.ResponseIntent{domain.IntentSoftGraylist, domain.IntentRateLimit, domain.IntentTemporaryQuarantine, domain.IntentTemporaryBlock} {
		t.Run(string(intent), func(t *testing.T) {
			action := endpointActionFixture(graph, resource, strings.Repeat("a", 64), domain.SignalSubjectV2{Type: "ip", Value: "203.0.113.10"}, intent, now, now.Add(time.Hour), "native")
			other := endpointActionFixture(graph, resource, strings.Repeat("b", 64), action.Action.Subject, intent, now, now.Add(30*time.Minute), "external")
			plan := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Actions: []EndpointActionInput{action, other}, Now: now})
			got := hostresources.Revision(struct {
				Plan      FirewallPlan
				Intent    any
				Candidate string
			}{plan, protectionIntent(plan), RenderManagedNFT(plan)})
			if got != want[intent] {
				t.Fatalf("accepted policy/intent/candidate changed: got %s, want %s", got, want[intent])
			}
		})
	}
	plan := applyPlan()
	got := hostresources.Revision(struct {
		Plan      FirewallPlan
		Intent    any
		Candidate string
	}{plan, protectionIntent(plan), RenderManagedNFT(plan)})
	if got != "0727d9c4efb87b7f8db8f3f61ae40d72ad4def682958032be9d34019d6f801c7" {
		t.Fatalf("accepted legacy policy/intent/candidate changed: %s", got)
	}
}

func TestProtectionPolicyPlanRoundTrip(t *testing.T) {
	plan := endpointTemporalEndpointPlan(time.Unix(1000, 0).UTC(), true)
	if err := Preflight(plan); err != nil {
		t.Fatal(err)
	}
	intent, err := json.Marshal(protectionIntent(plan))
	if err != nil {
		t.Fatal(err)
	}
	fixture := struct {
		Plan      FirewallPlan
		Intent    json.RawMessage
		Candidate string
	}{plan, intent, RenderManagedNFT(plan)}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var restored struct {
		Plan      FirewallPlan
		Intent    json.RawMessage
		Candidate string
	}
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(restored.Plan); !errors.Is(err, ErrPlanRevision) {
		t.Fatalf("serialized evidence must not revive current mutation authority: %v", err)
	}
	restoredIntent, err := json.Marshal(protectionIntent(restored.Plan))
	if err != nil || !bytes.Equal(restoredIntent, intent) || RenderManagedNFT(restored.Plan) != fixture.Candidate || restored.Plan.Revision != plan.Revision {
		t.Fatal("candidate serialization changed policy, revision, intent or rendered bytes")
	}
	// An optional qualification destination exports these actual production
	// values for the previous source adapter; it changes no runtime state.
	if destination := os.Getenv("SOLOVEY_PROTECTION_PLAN_FIXTURE"); destination != "" {
		if err := os.WriteFile(destination, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
