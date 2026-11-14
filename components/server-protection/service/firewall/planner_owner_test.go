package firewall

import "github.com/MalenkiySolovey/solovey-ui/components/server-protection/backendnetwork"

import (
	"context"
	"errors"
	"testing"

	componenthealth "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

func TestMissingNetworkPlannerRejectsBeforeStateAndHelperCalls(t *testing.T) {
	health := func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		t.Fatal("called health before Planner admission")
		return nil
	}
	workflow := Workflow{Manager: &operations.Manager{}, Helper: struct{ Helper }{}, Artifacts: struct{ ArtifactService }{}, Marker: struct{ MutationMarker }{}, State: struct{ StateStore }{}, Recovery: struct{ RecoveryBundler }{}, Contributions: struct{ FirewallContributionStore }{}, Health: health, RollbackHealth: health, Codec: backendnetwork.NFTCodec{}}
	if _, err := workflow.Prepare(t.Context(), PrepareInput{}); !errors.Is(err, ErrWorkflowDisabled) {
		t.Fatalf("unbound network owner admitted: %v", err)
	}
}
