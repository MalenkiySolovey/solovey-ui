package handoff

import (
	"context"
	"errors"

	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// portRecovery is used by the existing persisted operation-lock recovery
// runner. It only calls the injected typed adapters and can therefore be used
// in tests without process, listener or service access.
type portRecovery struct{ service *Service }

func (r portRecovery) HasMutationArtifact(ctx context.Context, item protectionrepository.OperationLockModel) (bool, error) {
	return r.service.Snapshot.HasMutation(ctx, Fence{OperationID: item.OperationID, Revision: item.Revision, InstanceID: r.service.Operations.InstanceID()})
}
func (r portRecovery) AttemptRollback(ctx context.Context, item protectionrepository.OperationLockModel) error {
	journal, err := r.service.Journal.PortOperation(ctx, item.OperationID)
	if err != nil {
		return err
	}
	previous, next, err := snapshots(journal)
	if err != nil {
		return err
	}
	_, fence, err := r.service.currentFence(ctx, item.OperationID, StateRollingBack)
	if err != nil {
		return err
	}
	coreMutation, inboundErr := r.service.Inbound.Rollback(ctx, previous, next, fence)
	_, fence, fenceErr := r.service.currentFence(ctx, item.OperationID, StateRollingBack)
	if fenceErr != nil {
		return errors.Join(inboundErr, fenceErr)
	}
	fallbackErr := r.service.Fallback.Rollback(ctx, previous, fence)
	restartErr := error(nil)
	if inboundErr != nil || !coreMutation.Restarted {
		_, fence, fenceErr = r.service.currentFence(ctx, item.OperationID, StateRollingBack)
		if fenceErr != nil {
			return errors.Join(inboundErr, fallbackErr, fenceErr)
		}
		restartErr = r.service.Restart.Restart(ctx, fence)
	}
	err = errors.Join(inboundErr, fallbackErr, restartErr)
	if err != nil {
		return err
	}
	return r.service.verifyRestored(ctx, previous, fence)
}
func (r portRecovery) CreateBundle(ctx context.Context, item protectionrepository.OperationLockModel, state string) error {
	journal, err := r.service.Journal.PortOperation(ctx, item.OperationID)
	if err != nil {
		return err
	}
	previous, next, err := snapshots(journal)
	if err != nil {
		return err
	}
	facts := recoveryFacts(item, previous, next)
	facts.State = state
	return r.service.Recovery.Record(ctx, facts)
}

var _ protectionoperations.Recovery = portRecovery{}
