package update

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

const OperatorManagedMode = "operator-managed"

var ErrOperatorManaged = errors.New("update activation is owned by the deployment operator")

// OperatorManagedProvider explicitly removes runtime replacement authority
// from an externally operated deployment. Signed release availability remains
// observable, but staging, activation, and rollback belong to the operator.
type OperatorManagedProvider struct{}

func NewOperatorManagedProvider() OperatorManagedProvider {
	return OperatorManagedProvider{}
}

func (OperatorManagedProvider) Capabilities(context.Context) Capabilities {
	result := Capabilities{Mode: OperatorManagedMode, Check: "AVAILABLE", Download: "UNAVAILABLE", Prepare: "UNAVAILABLE",
		Activate: "OPERATOR_MANAGED", Rollback: "OPERATOR_MANAGED", OSUpdates: "EXTERNAL_MANAGED", Reboot: "OPERATOR_ADVISORY",
		ReasonCodes: []string{"deployment_operator_owns_activation", "runtime_binary_activation_denied"}}
	result.Revision = semanticDigest(result)
	return result
}

func (OperatorManagedProvider) DownloadAndStage(context.Context, model.UpdateOperation, release.Verified, []release.Artifact, func(int64)) error {
	return ErrOperatorManaged
}

func (OperatorManagedProvider) Preflight(context.Context, model.UpdateOperation, release.Verified, []release.Artifact) (PreflightResult, error) {
	return PreflightResult{}, ErrOperatorManaged
}

func (OperatorManagedProvider) Activate(context.Context, model.UpdateOperation) error {
	return ErrOperatorManaged
}

func (OperatorManagedProvider) VerifyActive(context.Context, model.UpdateOperation) (bool, error) {
	return false, ErrOperatorManaged
}

func (OperatorManagedProvider) Rollback(context.Context, model.UpdateOperation) (bool, error) {
	return false, ErrOperatorManaged
}

func (OperatorManagedProvider) Reconcile(context.Context, model.UpdateOperation) (State, error) {
	return StateRecoveryRequired, ErrOperatorManaged
}
