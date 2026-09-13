package update

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

const (
	PackageManagedMode = "package-managed"
)

var ErrPackageManaged = errors.New("update activation is owned by the package deployment")

// PackageManagedProvider explicitly removes runtime binary replacement
// authority from a package-owned deployment. Signed release availability is
// still checked by LifecycleManager before a provider operation is considered.
type PackageManagedProvider struct{}

func NewPackageManagedProvider() PackageManagedProvider {
	return PackageManagedProvider{}
}

func (PackageManagedProvider) Capabilities(context.Context) Capabilities {
	result := Capabilities{Mode: PackageManagedMode, Check: "AVAILABLE", Download: "UNAVAILABLE", Prepare: "UNAVAILABLE",
		Activate: "PACKAGE_MANAGED", Rollback: "PACKAGE_MANAGED", OSUpdates: "PACKAGE_MANAGED", Reboot: "OPERATOR_ADVISORY",
		ReasonCodes: []string{"package_deployment_owns_activation", "runtime_binary_activation_denied"}}
	result.Revision = semanticDigest(result)
	return result
}

func (PackageManagedProvider) DownloadAndStage(context.Context, model.UpdateOperation, release.Verified, []release.Artifact, func(int64)) error {
	return ErrPackageManaged
}

func (PackageManagedProvider) Preflight(context.Context, model.UpdateOperation, release.Verified, []release.Artifact) (PreflightResult, error) {
	return PreflightResult{}, ErrPackageManaged
}

func (PackageManagedProvider) Activate(context.Context, model.UpdateOperation) error {
	return ErrPackageManaged
}

func (PackageManagedProvider) VerifyActive(context.Context, model.UpdateOperation) (bool, error) {
	return false, ErrPackageManaged
}

func (PackageManagedProvider) Rollback(context.Context, model.UpdateOperation) (bool, error) {
	return false, ErrPackageManaged
}

func (PackageManagedProvider) Reconcile(context.Context, model.UpdateOperation) (State, error) {
	return StateRecoveryRequired, ErrPackageManaged
}
