package handoff

import (
	"context"
	"errors"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func (s *Service) failAndRollback(ctx context.Context, item protectionrepository.PortOperationModel, lock protectionrepository.OperationLockModel, previous, next OwnerSnapshot, cause error) (protectionrepository.PortOperationModel, error) {
	rollbackCtx, cancel := detachedRollbackContext(ctx)
	defer cancel()
	current, _, currentErr := s.currentFence(rollbackCtx, lock.OperationID, StateApplying, StateHealth, StateHealthFailed)
	if currentErr != nil {
		return item, errors.Join(cause, currentErr)
	}
	lock = current
	rolling, transitionErr := s.Operations.Transition(rollbackCtx, lock.OperationID, lock.Revision, StateRollingBack)
	if transitionErr != nil {
		return item, errors.Join(cause, transitionErr)
	}
	updatedItem, journalErr := s.transitionJournal(rollbackCtx, item, []string{StateApplying, StateHealth, StateHealthFailed}, StateRollingBack, nil)
	if journalErr == nil {
		item = updatedItem
	}
	return s.rollbackFromRolling(ctx, item, rolling, previous, next, errors.Join(cause, journalErr))
}

func (s *Service) rollbackFromRolling(ctx context.Context, item protectionrepository.PortOperationModel, rolling protectionrepository.OperationLockModel, previous, next OwnerSnapshot, cause error) (protectionrepository.PortOperationModel, error) {
	if item.State != StateRollingBack {
		updatedItem, err := s.transitionJournal(ctx, item, []string{item.State}, StateRollingBack, nil)
		if err == nil {
			item = updatedItem
		} else {
			cause = errors.Join(cause, err)
		}
	}
	rollbackCtx, cancel := detachedRollbackContext(ctx)
	defer cancel()
	rolling, fence, fenceErr := s.currentFence(rollbackCtx, rolling.OperationID, StateRollingBack)
	if fenceErr != nil {
		return item, errors.Join(cause, fenceErr)
	}
	coreMutation, inboundErr := s.Inbound.Rollback(rollbackCtx, previous, next, fence)
	rolling, fence, fenceErr = s.currentFence(rollbackCtx, rolling.OperationID, StateRollingBack)
	if fenceErr != nil {
		return item, errors.Join(cause, inboundErr, fenceErr)
	}
	fallbackErr := s.Fallback.Rollback(rollbackCtx, previous, fence)
	restartErr := error(nil)
	if inboundErr != nil || !coreMutation.Restarted {
		rolling, fence, fenceErr = s.currentFence(rollbackCtx, rolling.OperationID, StateRollingBack)
		if fenceErr != nil {
			return item, errors.Join(cause, inboundErr, fallbackErr, fenceErr)
		}
		restartErr = s.Restart.Restart(rollbackCtx, fence)
	}
	rollbackErr := errors.Join(inboundErr, fallbackErr, restartErr)
	if rollbackErr == nil {
		rollbackErr = s.verifyRestored(rollbackCtx, previous, fence)
	}
	if rollbackErr != nil {
		if recoveryErr := s.Recovery.Record(rollbackCtx, recoveryFacts(rolling, previous, next)); recoveryErr != nil {
			return item, errors.Join(cause, rollbackErr, recoveryErr)
		}
		rolling, _, fenceErr = s.currentFence(rollbackCtx, rolling.OperationID, StateRollingBack)
		if fenceErr != nil {
			return item, errors.Join(cause, rollbackErr, fenceErr)
		}
		failed, finalErr := s.Operations.Transition(rollbackCtx, rolling.OperationID, rolling.Revision, StateRollbackFailed)
		if finalErr == nil {
			item, finalErr = s.transitionJournal(rollbackCtx, item, []string{StateRollingBack}, failed.State, nil)
		}
		return item, errors.Join(cause, rollbackErr, finalErr)
	}
	rolling, _, fenceErr = s.currentFence(rollbackCtx, rolling.OperationID, StateRollingBack)
	if fenceErr != nil {
		return item, errors.Join(cause, fenceErr)
	}
	rolled, finalErr := s.Operations.Transition(rollbackCtx, rolling.OperationID, rolling.Revision, StateRolledBack)
	if finalErr == nil {
		item, finalErr = s.transitionJournal(rollbackCtx, item, []string{StateRollingBack}, rolled.State, nil)
	}
	s.clearCancelled(rolling.OperationID)
	return item, errors.Join(cause, finalErr)
}

func (s *Service) healthFailedAndRollback(ctx context.Context, item protectionrepository.PortOperationModel, lock protectionrepository.OperationLockModel, previous, next OwnerSnapshot, healthJSON []byte, cause error) (protectionrepository.PortOperationModel, error) {
	rollbackCtx, cancel := detachedRollbackContext(ctx)
	defer cancel()
	failed, err := s.Operations.Transition(rollbackCtx, lock.OperationID, lock.Revision, StateHealthFailed)
	if err != nil {
		return item, errors.Join(cause, err)
	}
	updatedItem, err := s.transitionJournal(rollbackCtx, item, []string{StateHealth}, StateHealthFailed, healthJSON)
	if err != nil {
		return s.failAndRollback(rollbackCtx, item, failed, previous, next, errors.Join(cause, err))
	}
	item = updatedItem
	return s.failAndRollback(ctx, item, failed, previous, next, cause)
}

func (s *Service) abandon(ctx context.Context, item protectionrepository.PortOperationModel) (protectionrepository.PortOperationModel, error) {
	lock, err := s.lock(item.OperationID)
	if err != nil {
		return item, err
	}
	updated, err := s.Operations.Transition(ctx, item.OperationID, lock.Revision, StateAbandoned)
	if err != nil {
		return item, err
	}
	return s.transitionJournal(ctx, item, []string{StatePrepared}, updated.State, nil)
}
func (s *Service) cancelPrepared(ctx context.Context, item protectionrepository.PortOperationModel, lock protectionrepository.OperationLockModel) (protectionrepository.PortOperationModel, error) {
	updated, err := s.Operations.Transition(ctx, item.OperationID, lock.Revision, StateCancelled)
	if err != nil {
		return item, err
	}
	return s.transitionJournal(ctx, item, []string{StatePrepared}, updated.State, nil)
}
