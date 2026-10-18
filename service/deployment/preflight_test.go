package deployment

import (
	"testing"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

type unrecognizedPreflightProvider struct{ *fakeProvider }

func (*unrecognizedPreflightProvider) ProviderID() string { return "unknown-provider" }

func TestDeploymentPreviewConsumesCurrentPlatformFactsBeforeAdmission(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	preview, err := manager.Preview(t.Context(), domain.NativeHardened, true)
	if err != nil || !preview.Possible || !preview.Preflight.Ready || preview.Preflight.EvidenceRevision != preview.Posture.Revision {
		t.Fatalf("real preview missing bound platform facts: %+v %v", preview, err)
	}
	manager.Provider = &unrecognizedPreflightProvider{provider}
	denied, err := manager.Preview(t.Context(), domain.NativeHardened, true)
	if err != nil || denied.Possible || denied.Preflight.Ready {
		t.Fatalf("unknown provider admitted: %+v %v", denied, err)
	}
	_, err = manager.Start(t.Context(), StartRequest{TargetProfile: domain.NativeHardened, Acknowledged: true,
		IdempotencyKey: "unknown-provider-preflight", ExpectedPreviewRevision: denied.Revision, ExpectedPostureRevision: denied.Posture.Revision})
	if err == nil || len(provider.mutations) != 0 {
		t.Fatalf("failed prerequisite reached owner mutation: %v %v", provider.mutations, err)
	}
	var count int64
	if err := db.Table("deployment_operations_v1").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unknown provider admitted durable operation: %d %v", count, err)
	}
}
