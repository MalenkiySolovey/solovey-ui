package health

import (
	"context"
	"strings"
	"testing"

	componenthealth "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

type fakeChecker struct{ calls []string }

func (f *fakeChecker) Check(_ context.Context, resourceID string) componenthealth.Result {
	f.calls = append(f.calls, resourceID)
	return componenthealth.Result{ResourceID: resourceID, Status: componenthealth.StatusOK, Check: "internal", FactCode: "ready"}
}

func TestEvaluateUsesSafeOwnerChecksAndExactMissingCapability(t *testing.T) {
	checker := &fakeChecker{}
	results := EvaluateWithSSH(context.Background(), []hostresources.ProtectableResource{
		{ID: "core:subscription:default", Kind: "subscription"},
		{ID: "core:inbound:1", Kind: "inbound"},
		{ID: "core:panel:web", Kind: "panel_web"},
	}, checker, &fakeSSHChecker{kind: "ssh_management"})
	if got := strings.Join(checker.calls, ","); got != "core:panel:web,core:subscription:default" {
		t.Fatalf("safe check calls = %q", got)
	}
	if results[0].Status != componenthealth.StatusMissingCapability || results[0].FactCode != "inbound_listener_probe_unavailable" {
		t.Fatalf("inbound result = %#v", results[0])
	}
}

func TestHealthOutputContainsNoConfiguredPath(t *testing.T) {
	results := EvaluateWithSSH(context.Background(), []hostresources.ProtectableResource{{ID: "core:subscription:default", Kind: "subscription"}}, &fakeChecker{}, &fakeSSHChecker{kind: "ssh_management"})
	if strings.Contains(strings.ToLower(results[0].FactCode), "path") || strings.Contains(strings.ToLower(results[0].FactCode), "url") {
		t.Fatalf("unsafe health fact = %#v", results[0])
	}
}

type fakeSSHChecker struct {
	kind   string
	reads  int
	checks int
}

func (checker *fakeSSHChecker) ResourceKind() string { checker.reads++; return checker.kind }

func (checker *fakeSSHChecker) Check(_ context.Context, resources []hostresources.ProtectableResource) []componenthealth.Result {
	checker.checks++
	results := make([]componenthealth.Result, 0, len(resources))
	for _, resource := range resources {
		results = append(results, componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusDegraded, Check: "ssh_owner", FactCode: "fixture_owner_unavailable"})
	}
	return results
}

func TestMissingHealthOwnerCannotFallBackToGenericChecks(t *testing.T) {
	for _, absent := range []string{"registered", "ssh", "kind"} {
		t.Run(absent, func(t *testing.T) {
			ordinary, owner := &fakeChecker{}, &fakeSSHChecker{kind: "ssh_management"}
			var checks Checker = ordinary
			var ssh SSHChecker = owner
			switch absent {
			case "registered":
				checks = nil
			case "ssh":
				ssh = nil
			case "kind":
				owner.kind = ""
			}
			results := EvaluateWithSSH(t.Context(), []hostresources.ProtectableResource{{ID: "ssh", Kind: "ssh_management"}, {ID: "panel", Kind: "panel_web"}}, checks, ssh)
			if len(results) != 2 || len(ordinary.calls) != 0 || owner.checks != 0 {
				t.Fatalf("missing owner reached checks: %#v", results)
			}
			for _, result := range results {
				if result.Status != componenthealth.StatusMissingCapability || result.FactCode != "health_owner_unavailable" {
					t.Fatalf("missing owner reported readiness: %#v", result)
				}
			}
		})
	}
}

func TestEmptyHealthInventoryDoesNotInvokeAnyOwner(t *testing.T) {
	ordinary, ssh := &fakeChecker{}, &fakeSSHChecker{kind: "ssh_management"}
	if results := EvaluateWithSSH(t.Context(), nil, ordinary, ssh); len(results) != 0 || len(ordinary.calls) != 0 || ssh.reads != 0 || ssh.checks != 0 {
		t.Fatal("empty inventory observed an owner")
	}
}

func TestSSHHealthUsesOneTypedOwnerBatch(t *testing.T) {
	ordinary, ssh := &fakeChecker{}, &fakeSSHChecker{kind: "ssh_management"}
	results := EvaluateWithSSH(t.Context(), []hostresources.ProtectableResource{{ID: "ssh-b", Kind: "ssh_management"}, {ID: "panel", Kind: "panel_web"}, {ID: "ssh-a", Kind: "ssh_management"}}, ordinary, ssh)
	if len(results) != 3 || ssh.reads != 1 || ssh.checks != 1 || strings.Join(ordinary.calls, ",") != "panel" || Summary(results) != componenthealth.StatusDegraded {
		t.Fatalf("SSH authority was split or replaced: %#v", results)
	}
}
