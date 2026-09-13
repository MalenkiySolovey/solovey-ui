package update

import (
	"context"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func TestPackageManagedCapabilitiesSeparateObservationFromActivation(t *testing.T) {
	provider := NewPackageManagedProvider()
	capabilities := provider.Capabilities(context.Background())
	if capabilities.Mode != PackageManagedMode || capabilities.Check != "AVAILABLE" ||
		capabilities.Download != "UNAVAILABLE" || capabilities.Prepare != "UNAVAILABLE" ||
		capabilities.Activate != "PACKAGE_MANAGED" || capabilities.Rollback != "PACKAGE_MANAGED" ||
		capabilities.OSUpdates != "PACKAGE_MANAGED" || capabilities.Revision == "" {
		t.Fatalf("package-managed capability contract drifted: %#v", capabilities)
	}
	if ReasonCode(ErrPackageManaged) != "update_package_managed" {
		t.Fatalf("package-managed reason code drifted: %q", ReasonCode(ErrPackageManaged))
	}
}

func TestPackageManagedProviderDeniesRuntimeBinaryMutation(t *testing.T) {
	provider := NewPackageManagedProvider()
	ctx := context.Background()
	operation := model.UpdateOperation{}
	verified := release.Verified{}
	artifacts := []release.Artifact{}
	if err := provider.DownloadAndStage(ctx, operation, verified, artifacts, func(int64) {}); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("download/stage authority was granted: %v", err)
	}
	if _, err := provider.Preflight(ctx, operation, verified, artifacts); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("preflight authority was granted: %v", err)
	}
	if err := provider.Activate(ctx, operation); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("native activation authority was granted: %v", err)
	}
	if _, err := provider.VerifyActive(ctx, operation); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("runtime activation verification authority was granted: %v", err)
	}
	if _, err := provider.Rollback(ctx, operation); !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("runtime rollback authority was granted: %v", err)
	}
	if state, err := provider.Reconcile(ctx, operation); state != StateRecoveryRequired || !errors.Is(err, ErrPackageManaged) {
		t.Fatalf("package-managed reconciliation=%s/%v", state, err)
	}
}
