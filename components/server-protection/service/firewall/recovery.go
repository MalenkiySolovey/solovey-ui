package firewall

import (
	"context"
	"errors"

	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// BackendRecovery resumes only the recorded rollback. It never repeats apply
// and cannot synthesize a new mutation from request or UI data.
type BackendRecovery struct {
	Helper     Helper
	Manager    *protectionoperations.Manager
	Storage    *protectionartifacts.Storage
	Repository *protectionrepository.Repository
	Health     HealthCheck
	Workflow   *Workflow
}

func (r BackendRecovery) HasMutationArtifact(ctx context.Context, operation protectionrepository.OperationLockModel) (bool, error) {
	if r.Storage == nil || r.Repository == nil || !r.Storage.HasMutationMarker(operation.OperationID) {
		return false, nil
	}
	artifact, err := r.Repository.ArtifactByOperation(ctx, operation.OperationID)
	if err != nil {
		return false, err
	}
	_, err = r.Storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256)
	return err == nil, err
}

func (r BackendRecovery) AttemptRollback(ctx context.Context, operation protectionrepository.OperationLockModel) error {
	if r.Helper == nil || r.Manager == nil || r.Storage == nil || r.Repository == nil || r.Workflow == nil {
		return errors.New("restricted rollback backend is unavailable")
	}
	artifact, err := r.Repository.ArtifactByOperation(ctx, operation.OperationID)
	if err != nil {
		return err
	}
	if manifest, verifyErr := r.Storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256); verifyErr != nil || manifest.OperationID != operation.OperationID {
		return errors.Join(errors.New("firewall recovery artifact identity is invalid"), verifyErr)
	}
	_, err = r.Workflow.finishRollback(ctx, operation, artifact.Revision, true, false)
	return err
}

func (r BackendRecovery) CreateBundle(ctx context.Context, operation protectionrepository.OperationLockModel, state string) error {
	return (protectionartifacts.OperationRecovery{Storage: r.Storage, Repository: r.Repository}).CreateBundle(ctx, operation, state)
}
