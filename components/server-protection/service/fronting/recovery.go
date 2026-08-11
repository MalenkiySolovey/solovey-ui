package fronting

import (
	"context"
	"errors"
	"fmt"
	"time"

	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// BackendRecovery resumes only the exact previous managed revision recorded by
// Sync. It verifies actual state first and never repeats forward switch/apply.
type BackendRecovery struct {
	Helper     Helper
	Manager    *protectionoperations.Manager
	Storage    *protectionartifacts.Storage
	Repository *protectionrepository.Repository
	Health     HealthCheck
}

func (r BackendRecovery) HasMutationArtifact(ctx context.Context, operation protectionrepository.OperationLockModel) (bool, error) {
	if r.Storage == nil || r.Repository == nil || !r.Storage.HasMutationMarker(operation.OperationID) {
		return false, nil
	}
	artifact, err := r.Repository.ArtifactByOperation(ctx, operation.OperationID)
	if err != nil {
		return false, err
	}
	if _, err = r.Storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256); err != nil {
		return false, err
	}
	_, err = r.Storage.ReadFrontingState(operation.OperationID)
	return err == nil, err
}

func (r BackendRecovery) AttemptRollback(ctx context.Context, operation protectionrepository.OperationLockModel) error {
	if r.Helper == nil || r.Manager == nil || r.Storage == nil || r.Health == nil {
		return errors.New("restricted fronting rollback backend is unavailable")
	}
	workflow := &Workflow{Manager: r.Manager, Helper: r.Helper, State: r.Storage, Health: r.Health, RollbackHealth: r.Health, Now: time.Now}
	checkpoint, err := workflow.load(operation.OperationID)
	if err != nil {
		return errors.Join(errors.New("fronting mutation checkpoint is unavailable"), err)
	}
	capabilities, err := workflow.capabilities(ctx)
	if err != nil || !frontingCapabilitiesAvailable(capabilities) || capabilities.Nginx.Binary != checkpoint.Binary || capabilities.Nginx.ManagedRoot != checkpoint.ManagedRoot || capabilities.Nginx.ControlledConfig != checkpoint.ControlledConfig {
		return errors.Join(ErrMissingCapability, err)
	}
	if !checkpoint.Switched {
		switch {
		case capabilities.Nginx.ActiveRevision == checkpoint.PreviousRevision && capabilities.Nginx.ActiveSHA256 == checkpoint.PreviousSHA256:
			return nil
		case capabilities.Nginx.ActiveRevision == checkpoint.DesiredRevision && capabilities.Nginx.ActiveSHA256 == checkpoint.CandidateSHA256:
			checkpoint.Switched = true
			if err := workflow.save(&checkpoint, checkpointSwitched); err != nil {
				return err
			}
		default:
			return ErrActiveRevision
		}
	}
	correlation := workflow.correlation(operation)
	if capabilities.Nginx.ActiveRevision == checkpoint.DesiredRevision {
		response, restoreErr := workflow.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxRestore, NginxRestore: &protectionhelper.NginxRestoreRequest{ExpectedCurrentRevision: checkpoint.DesiredRevision, PreviousRevision: checkpoint.PreviousRevision, ExpectedSHA256: checkpoint.PreviousSHA256}})
		if restoreErr != nil || response.Nginx == nil || response.Nginx.Revision != checkpoint.PreviousRevision {
			return errors.Join(errors.New("fronting recovery restore failed"), restoreErr)
		}
		checkpoint.Restored = true
		if err := workflow.save(&checkpoint, checkpointRestored); err != nil {
			return err
		}
	} else if capabilities.Nginx.ActiveRevision != checkpoint.PreviousRevision || capabilities.Nginx.ActiveSHA256 != checkpoint.PreviousSHA256 {
		return ErrActiveRevision
	} else {
		checkpoint.Restored = true
	}
	if !checkpoint.RollbackReloaded {
		response, reloadErr := workflow.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxReload, NginxReload: &protectionhelper.NginxReloadRequest{ExpectedRevision: checkpoint.PreviousRevision, ExpectedSHA256: checkpoint.PreviousSHA256, ExpectedBinary: checkpoint.Binary}})
		if reloadErr != nil || response.Nginx == nil || response.Nginx.MasterPID <= 0 {
			return errors.Join(ErrRollbackReload, reloadErr)
		}
		checkpoint.RollbackReloaded = true
		if err := workflow.save(&checkpoint, checkpointRollbackReload); err != nil {
			return err
		}
	}
	response, err := workflow.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxVerify, NginxVerify: &protectionhelper.NginxVerifyRequest{ExpectedRevision: checkpoint.PreviousRevision, ExpectedSHA256: checkpoint.PreviousSHA256, ExpectedBinary: checkpoint.Binary, Listeners: checkpoint.PreviousListeners}})
	if err != nil || response.Nginx == nil || !response.Nginx.ListenersMatched || response.Nginx.Revision != checkpoint.PreviousRevision || response.Nginx.SHA256 != checkpoint.PreviousSHA256 || response.Nginx.Binary != checkpoint.Binary || response.Nginx.MasterPID <= 0 || len(response.Nginx.WorkerPIDs) == 0 {
		return errors.Join(fmt.Errorf("fronting recovery active verification failed"), err)
	}
	checkpoint.RollbackHealth = boundedHealth(ctx, r.Health, nil, "fronting_recovery_health_timeout")
	if healthFailed(checkpoint.RollbackHealth) {
		return ErrRollbackHealth
	}
	return workflow.save(&checkpoint, checkpointRollbackHealth)
}

func (r BackendRecovery) CreateBundle(ctx context.Context, operation protectionrepository.OperationLockModel, state string) error {
	return (protectionartifacts.OperationRecovery{Storage: r.Storage, Repository: r.Repository}).CreateBundle(ctx, operation, state)
}
