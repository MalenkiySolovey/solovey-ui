package firewall

import (
	"context"
	"errors"
	"testing"

	componenthealth "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

func TestMissingArtifactOwnerRejectsBeforeAnyStateOrHelperRead(t *testing.T) {
	health := func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
		t.Fatal("health called before codec admission")
		return nil
	}
	workflow := Workflow{Codec: nil, NetworkPlanner: struct{ NetworkPlanner }{}, Manager: &operations.Manager{}, Helper: struct{ Helper }{}, Artifacts: struct{ ArtifactService }{}, Marker: struct{ MutationMarker }{}, State: struct{ StateStore }{}, Recovery: struct{ RecoveryBundler }{}, Contributions: struct{ FirewallContributionStore }{}, Health: health, RollbackHealth: health}
	if _, err := workflow.Prepare(t.Context(), PrepareInput{}); !errors.Is(err, ErrWorkflowDisabled) {
		t.Fatalf("missing artifact owner admitted: %v", err)
	}
	baseline := NewBaselineService(struct{ BaselineStore }{}, struct{ SSHPostureSource }{}, nil, unreadBaselineSources(t))
	if _, err := baseline.Snapshot(t.Context(), true, nil); !errors.Is(err, ErrMissingCapability) {
		t.Fatalf("baseline used implicit artifact owner: %v", err)
	}
}

func TestMissingArtifactOwnerCannotMaterializePreviewOrCandidate(t *testing.T) {
	preview := Preview(FirewallPlan{}, PreviewOptions{IncludeGeneratedNFT: true, NFTCapability: NFTPreviewCapability{Available: true, Revision: "observed"}, Codec: nil})
	if preview.Backend != "unsupported" || preview.GeneratedNFT != "" {
		t.Fatalf("missing codec produced an actionable preview: %#v", preview)
	}
	if RenderManagedNFT(FirewallPlan{}, nil) != "" || RenderNFTPreview(FirewallPlan{}, nil) != "" {
		t.Fatal("implicit owner materialized a candidate")
	}
	if err := Preflight(FirewallPlan{}, nil); !errors.Is(err, ErrMissingCapability) {
		t.Fatalf("missing codec passed admission: %v", err)
	}
}
