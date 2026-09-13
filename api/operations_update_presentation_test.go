package api

import (
	"testing"

	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
)

func TestDockerUpdateModeCompatibilityIsPresentationOnly(t *testing.T) {
	status := updateservice.LifecycleStatus{
		Actual:       updateservice.UpdateActualState{Mode: updateservice.OperatorManagedMode},
		Capabilities: updateservice.NewOperatorManagedProvider().Capabilities(t.Context()),
	}
	presented := projectUpdatePresentation(status, deploymentservice.UpdatePresentation{LegacyMode: "docker-operator-managed",
		LegacyReasonCodes: []string{"docker_runtime_operator_managed", "docker_socket_not_used"}})
	if presented.Actual.Mode != "docker-operator-managed" {
		t.Fatalf("released Docker display mode was not preserved: %#v", presented.Actual)
	}
	if presented.Capabilities.Mode != "docker-operator-managed" || presented.Capabilities.Revision == status.Capabilities.Revision {
		t.Fatalf("released Docker capability presentation was not projected coherently: %#v", presented.Capabilities)
	}
	neutral := projectUpdatePresentation(status, deploymentservice.UpdatePresentation{})
	if neutral.Actual.Mode != updateservice.OperatorManagedMode {
		t.Fatalf("non-Docker operator-managed deployment was branded: %#v", neutral.Actual)
	}
	if status.Capabilities.Mode != updateservice.OperatorManagedMode {
		t.Fatalf("presentation mutated portable Update semantics: %#v", status.Capabilities)
	}
}
