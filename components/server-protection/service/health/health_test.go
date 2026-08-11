package health

import (
	"context"
	"strings"
	"testing"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type fakeChecker struct{ calls []string }

func (f *fakeChecker) Check(_ context.Context, resourceID string) componenthealth.Result {
	f.calls = append(f.calls, resourceID)
	return componenthealth.Result{ResourceID: resourceID, Status: componenthealth.StatusOK, Check: "internal", FactCode: "ready"}
}

func TestEvaluateUsesSafeOwnerChecksAndExactMissingCapability(t *testing.T) {
	checker := &fakeChecker{}
	results := Evaluate(context.Background(), []hostresources.ProtectableResource{
		{ID: "core:subscription:default", Kind: "subscription"},
		{ID: "core:inbound:1", Kind: "inbound"},
		{ID: "core:panel:web", Kind: "panel_web"},
	}, checker)
	if got := strings.Join(checker.calls, ","); got != "core:panel:web,core:subscription:default" {
		t.Fatalf("safe check calls = %q", got)
	}
	if results[0].Status != componenthealth.StatusMissingCapability || results[0].FactCode != "inbound_listener_probe_unavailable" {
		t.Fatalf("inbound result = %#v", results[0])
	}
}

func TestHealthOutputContainsNoConfiguredPath(t *testing.T) {
	results := Evaluate(context.Background(), []hostresources.ProtectableResource{{ID: "core:subscription:default", Kind: "subscription"}}, &fakeChecker{})
	if strings.Contains(strings.ToLower(results[0].FactCode), "path") || strings.Contains(strings.ToLower(results[0].FactCode), "url") {
		t.Fatalf("unsafe health fact = %#v", results[0])
	}
}
