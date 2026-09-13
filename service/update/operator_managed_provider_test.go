package update

import (
	"context"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
)

func TestOperatorManagedCapabilitiesAreDeploymentNeutral(t *testing.T) {
	provider := NewOperatorManagedProvider()
	capabilities := provider.Capabilities(context.Background())
	if capabilities.Mode != OperatorManagedMode || capabilities.Check != "AVAILABLE" ||
		capabilities.Download != "UNAVAILABLE" || capabilities.Prepare != "UNAVAILABLE" ||
		capabilities.Activate != "OPERATOR_MANAGED" || capabilities.Rollback != "OPERATOR_MANAGED" ||
		capabilities.OSUpdates != "EXTERNAL_MANAGED" || capabilities.Revision == "" {
		t.Fatalf("operator-managed capability contract drifted: %#v", capabilities)
	}
	for _, value := range append([]string{capabilities.Mode}, capabilities.ReasonCodes...) {
		if value == "docker" || value == "docker-operator-managed" || value == "docker_runtime_operator_managed" || value == "docker_socket_not_used" {
			t.Fatalf("operator-managed capability contains deployment brand %q", value)
		}
	}
	if ReasonCode(ErrOperatorManaged) != "update_operator_managed" {
		t.Fatalf("operator-managed reason code drifted: %q", ReasonCode(ErrOperatorManaged))
	}
}

func TestOperatorManagedProviderDeniesRuntimeBinaryMutation(t *testing.T) {
	provider := NewOperatorManagedProvider()
	ctx := context.Background()
	operation := model.UpdateOperation{}
	verified := release.Verified{}
	artifacts := []release.Artifact{}
	if err := provider.DownloadAndStage(ctx, operation, verified, artifacts, func(int64) {}); !errors.Is(err, ErrOperatorManaged) {
		t.Fatalf("download/stage authority was granted: %v", err)
	}
	if _, err := provider.Preflight(ctx, operation, verified, artifacts); !errors.Is(err, ErrOperatorManaged) {
		t.Fatalf("preflight authority was granted: %v", err)
	}
	if err := provider.Activate(ctx, operation); !errors.Is(err, ErrOperatorManaged) {
		t.Fatalf("activation authority was granted: %v", err)
	}
	if _, err := provider.Rollback(ctx, operation); !errors.Is(err, ErrOperatorManaged) {
		t.Fatalf("rollback authority was granted: %v", err)
	}
}

func TestLifecycleSelectionConsumesOperatorManagedSemantics(t *testing.T) {
	provider := providerForUpdateLifecycle(deploymentservice.UpdateLifecycleOperatorManaged, nil)
	if _, ok := provider.(OperatorManagedProvider); !ok {
		t.Fatalf("operator-managed lifecycle selected %T", provider)
	}
	if provider.Capabilities(context.Background()).Mode != OperatorManagedMode {
		t.Fatalf("operator-managed lifecycle selected deployment-specific capabilities: %#v", provider.Capabilities(context.Background()))
	}
}

func TestLifecycleSelectionConsumesEveryNeutralDeploymentFact(t *testing.T) {
	selfManaged := &BrokerProvider{}
	fixtures := []struct {
		name      string
		lifecycle deploymentservice.UpdateLifecycle
		want      any
	}{
		{"self-managed", deploymentservice.UpdateLifecycleSelfManaged, selfManaged},
		{"package-managed", deploymentservice.UpdateLifecyclePackageManaged, PackageManagedProvider{}},
		{"operator-managed", deploymentservice.UpdateLifecycleOperatorManaged, OperatorManagedProvider{}},
		{"unavailable", deploymentservice.UpdateLifecycleUnavailable, UnavailableProvider{}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			got := providerForUpdateLifecycle(fixture.lifecycle, selfManaged)
			switch fixture.want.(type) {
			case *BrokerProvider:
				if got != selfManaged {
					t.Fatalf("self-managed lifecycle selected %T", got)
				}
			case PackageManagedProvider:
				if _, ok := got.(PackageManagedProvider); !ok {
					t.Fatalf("package-managed lifecycle selected %T", got)
				}
			case OperatorManagedProvider:
				if _, ok := got.(OperatorManagedProvider); !ok {
					t.Fatalf("operator-managed lifecycle selected %T", got)
				}
			case UnavailableProvider:
				if _, ok := got.(UnavailableProvider); !ok {
					t.Fatalf("unavailable lifecycle selected %T", got)
				}
			}
		})
	}
}

func TestSharedLifecycleCompositionDoesNotUseHostGOOSAsSemanticSelector(t *testing.T) {
	fixtures := []struct {
		kind string
		want any
	}{
		{"native", (*BrokerProvider)(nil)},
		{deploymentservice.OpenWrtPackageManagedDeploymentKind, PackageManagedProvider{}},
		{"docker", OperatorManagedProvider{}},
		{"unknown", UnavailableProvider{}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.kind, func(t *testing.T) {
			t.Setenv("SUI_DEPLOYMENT_KIND", fixture.kind)
			got := newSharedLifecycle().provider
			switch fixture.want.(type) {
			case *BrokerProvider:
				if _, ok := got.(*BrokerProvider); !ok {
					t.Fatalf("self-managed deployment selected %T", got)
				}
			case PackageManagedProvider:
				if _, ok := got.(PackageManagedProvider); !ok {
					t.Fatalf("package-managed deployment selected %T", got)
				}
			case OperatorManagedProvider:
				if _, ok := got.(OperatorManagedProvider); !ok {
					t.Fatalf("operator-managed deployment selected %T", got)
				}
			case UnavailableProvider:
				if _, ok := got.(UnavailableProvider); !ok {
					t.Fatalf("unavailable deployment selected %T", got)
				}
			}
		})
	}
}
