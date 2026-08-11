package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func NewService(journal JournalStore, manager *protectionoperations.Manager) *Service {
	s := &Service{Journal: journal, Operations: manager, Now: time.Now, cancelled: map[string]bool{}}
	// Recovery invokes only the typed adapters configured for this service.
	if manager != nil {
		s.initErr = manager.SetRecoveryForKind(protectionoperations.KindPortHandoff, portRecovery{service: s})
	}
	return s
}

func (s *Service) Prepare(ctx context.Context, plan Plan, confirmation string) (protectionrepository.PortOperationModel, bool, error) {
	if err := s.ready(); err != nil {
		return protectionrepository.PortOperationModel{}, false, err
	}
	if err := s.preflight(ctx, plan); err != nil {
		return protectionrepository.PortOperationModel{}, false, err
	}
	if confirmation != "PREPARE PORT HANDOFF "+plan.PlanRevision {
		return protectionrepository.PortOperationModel{}, false, protectionoperations.ErrConfirmationRequired
	}
	acquired, err := s.Operations.Prepare(ctx, protectionoperations.PrepareRequest{PlanRevision: plan.PlanRevision, Confirmation: "PREPARE SERVER PROTECTION " + plan.PlanRevision, Acquire: protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindPortHandoff, ResourceID: plan.Previous.ResourceID, Protocol: plan.Previous.Protocol, Listen: plan.Previous.Listen, Port: intPtr(plan.Previous.Port), IdempotencyKey: plan.IdempotencyKey, Actor: plan.Actor,
	}})
	if err != nil {
		return protectionrepository.PortOperationModel{}, false, err
	}
	if acquired.Joined {
		item, loadErr := s.Journal.PortOperation(ctx, acquired.Operation.OperationID)
		return item, true, loadErr
	}
	previous, next := cloneSnapshot(plan.Previous), cloneSnapshot(plan.Next)
	previousRaw, err := json.Marshal(previous)
	if err != nil {
		return protectionrepository.PortOperationModel{}, false, err
	}
	nextRaw, err := json.Marshal(next)
	if err != nil {
		return protectionrepository.PortOperationModel{}, false, err
	}
	healthTargetsRaw, err := json.Marshal(plan.HealthTargets)
	if err != nil {
		return protectionrepository.PortOperationModel{}, false, err
	}
	now := s.now().Unix()
	item, joined, err := s.Journal.CreatePortOperation(ctx, protectionrepository.PortOperationModel{
		OperationID: acquired.Operation.OperationID, IdempotencyKey: plan.IdempotencyKey, Revision: 1, State: StatePrepared, FromOwner: previous.Owner, ToOwner: next.Owner, Protocol: previous.Protocol, Listen: previous.Listen, Port: previous.Port,
		PreviousResourceJSON: previousRaw, NextResourceJSON: nextRaw, PreviousResourceRevision: previous.ResourceRevision, NextResourceRevision: next.ResourceRevision, PreviousConfigRevision: previous.ConfigRevision, NextConfigRevision: next.ConfigRevision,
		CoreConfigRevision: next.ConfigRevision, FallbackTargetRevision: previous.ConfigRevision, HealthTargetsJSON: healthTargetsRaw, CreatedAt: now, UpdatedAt: now, PreparedAt: &now,
	})
	return item, joined, err
}

func (s *Service) Apply(ctx context.Context, operationID, confirmation string) (protectionrepository.PortOperationModel, error) {
	if err := s.ready(); err != nil {
		return protectionrepository.PortOperationModel{}, err
	}
	if confirmation != "APPLY PORT HANDOFF "+operationID {
		return protectionrepository.PortOperationModel{}, protectionoperations.ErrConfirmationRequired
	}
	item, err := s.Journal.PortOperation(ctx, operationID)
	if err != nil {
		return protectionrepository.PortOperationModel{}, err
	}
	if item.State == StateApplied || item.State == StateRolledBack || item.State == StateCancelled {
		return item, nil
	}
	if item.State != StatePrepared {
		return item, fmt.Errorf("port handoff cannot apply from %s", item.State)
	}
	previous, next, err := snapshots(item)
	if err != nil {
		return item, err
	}
	lock, err := s.lock(operationID)
	if err != nil {
		return item, err
	}
	fence := fenceFromLock(lock, s.Operations.InstanceID())
	if err := s.revalidate(ctx, previous, next); err != nil {
		if errors.Is(err, ErrOwnerDisappeared) {
			abandoned, abandonErr := s.abandon(ctx, item)
			return abandoned, errors.Join(ErrOwnerDisappeared, abandonErr)
		}
		return item, err
	}
	if err := s.Snapshot.Checkpoint(ctx, previous, next, fence); err != nil {
		return item, err
	}
	if err := s.Inbound.Prepare(ctx, next, fence); err != nil {
		return item, errors.Join(err, s.abortPrepared(ctx, next, previous, fence))
	}
	if err := s.Fallback.Prepare(ctx, previous, fence); err != nil {
		return item, errors.Join(err, s.abortPrepared(ctx, next, previous, fence))
	}
	if s.cancelledBeforeBoundary(operationID) {
		if err := s.abortPrepared(ctx, next, previous, fence); err != nil {
			return item, err
		}
		return s.cancelPrepared(ctx, item, lock)
	}
	// Target preparation may take long enough for an owner edit or a new
	// listener to appear. Recheck immediately before the mutation marker and
	// source release; owner adapters must additionally CAS the revisions passed
	// to ReleasePublicListener.
	if err := s.validateFence(ctx, fence); err != nil {
		return item, errors.Join(err, s.abortPrepared(ctx, next, previous, fence))
	}
	if err := s.revalidate(ctx, previous, next); err != nil {
		abortErr := s.abortPrepared(ctx, next, previous, fence)
		if errors.Is(err, ErrOwnerDisappeared) && abortErr == nil {
			abandoned, abandonErr := s.abandon(ctx, item)
			return abandoned, errors.Join(err, abandonErr)
		}
		return item, errors.Join(err, abortErr)
	}
	applying, err := s.Operations.Transition(ctx, operationID, lock.Revision, StateApplying)
	if err != nil {
		return item, err
	}
	fence.Revision = applying.Revision
	updatedItem, err := s.transitionJournal(ctx, item, []string{StatePrepared}, StateApplying, nil)
	if err != nil {
		abortErr := s.abortPrepared(ctx, next, previous, fence)
		abandonCtx, cancel := detachedRollbackContext(ctx)
		defer cancel()
		abandoned, abandonErr := s.Operations.Transition(abandonCtx, operationID, applying.Revision, StateAbandoned)
		if abandonErr == nil {
			item, abandonErr = s.transitionJournal(abandonCtx, item, []string{StatePrepared}, abandoned.State, nil)
		}
		return item, errors.Join(err, abortErr, abandonErr)
	}
	item = updatedItem
	applying, fence, err = s.currentFence(ctx, operationID, StateApplying)
	if err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, err)
	}
	if err := s.Snapshot.MarkMutation(ctx, fence); err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, err)
	}
	applying, fence, err = s.currentFence(ctx, operationID, StateApplying)
	if err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, err)
	}
	if err := s.Fallback.Apply(ctx, previous, fence); err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, err)
	}
	if s.cancelledAfterBoundary(operationID) {
		return s.failAndRollback(ctx, item, applying, previous, next, ErrCancelled)
	}
	applying, fence, err = s.currentFence(ctx, operationID, StateApplying)
	if err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, err)
	}
	coreMutation, err := s.Inbound.Apply(ctx, next, fence)
	if err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, err)
	}
	if !coreMutation.Restarted {
		applying, fence, err = s.currentFence(ctx, operationID, StateApplying)
		if err != nil {
			return s.failAndRollback(ctx, item, applying, previous, next, err)
		}
		if err := s.Restart.Restart(ctx, fence); err != nil {
			return s.failAndRollback(ctx, item, applying, previous, next, err)
		}
	}
	if err := s.verifyExactOwner(ctx, next, fence); err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, errors.Join(ErrListenerVerify, err))
	}
	if s.cancelledAfterBoundary(operationID) {
		return s.failAndRollback(ctx, item, applying, previous, next, ErrCancelled)
	}
	applying, _, err = s.currentFence(ctx, operationID, StateApplying)
	if err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, err)
	}
	healthLock, err := s.Operations.Transition(ctx, operationID, applying.Revision, StateHealth)
	if err != nil {
		return s.failAndRollback(ctx, item, applying, previous, next, err)
	}
	updatedItem, err = s.transitionJournal(ctx, item, []string{StateApplying}, StateHealth, nil)
	if err != nil {
		return s.failAndRollback(ctx, item, healthLock, previous, next, err)
	}
	item = updatedItem
	targets := journalHealthTargets(item, previous, next)
	results, healthErr := s.Health.Check(ctx, targets)
	results = safeHealthResults(results)
	healthJSON, _ := json.Marshal(results)
	if healthErr != nil || !healthyFor(targets, results) {
		return s.healthFailedAndRollback(ctx, item, healthLock, previous, next, healthJSON, errors.Join(ErrHealth, healthErr))
	}
	healthLock, _, err = s.currentFence(ctx, operationID, StateHealth)
	if err != nil {
		return s.failAndRollback(ctx, item, healthLock, previous, next, err)
	}
	applied, err := s.Operations.Transition(ctx, operationID, healthLock.Revision, StateApplied)
	if err != nil {
		return s.failAndRollback(ctx, item, healthLock, previous, next, err)
	}
	commitCtx, cancelCommit := detachedRollbackContext(ctx)
	defer cancelCommit()
	committed, commitErr := s.transitionJournal(commitCtx, item, []string{StateHealth}, applied.State, healthJSON)
	if commitErr == nil {
		s.clearCancelled(operationID)
		return committed, nil
	}
	// The owner is healthy but the journal commit failed after the global lock
	// became terminal. Reclaim the applied lock and restore the exact previous
	// owner instead of leaving an unjournaled takeover behind.
	rolling, reclaimErr := s.Operations.BeginRollback(commitCtx, operationID, applied.Revision)
	if reclaimErr != nil {
		return item, errors.Join(commitErr, reclaimErr)
	}
	return s.rollbackFromRolling(commitCtx, item, rolling, previous, next, commitErr)
}

func (s *Service) Cancel(ctx context.Context, operationID string) (protectionrepository.PortOperationModel, error) {
	item, err := s.Journal.PortOperation(ctx, operationID)
	if err != nil {
		return protectionrepository.PortOperationModel{}, err
	}
	if item.State == StatePrepared {
		lock, lockErr := s.lock(operationID)
		if lockErr != nil {
			return item, lockErr
		}
		return s.cancelPrepared(ctx, item, lock)
	}
	if item.State == StateApplying || item.State == StateHealth || item.State == StateHealthFailed {
		s.mu.Lock()
		s.cancelled[operationID] = true
		s.mu.Unlock()
		return item, nil
	}
	return item, fmt.Errorf("port handoff cannot cancel from %s", item.State)
}

func (s *Service) Recover(ctx context.Context) ([]protectionrepository.PortOperationModel, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if _, err := s.Operations.Recover(ctx); err != nil {
		return nil, err
	}
	items, err := s.Journal.ListPortOperations(ctx, []string{StatePrepared, StateApplying, StateHealth, StateHealthFailed, StateRollingBack})
	if err != nil {
		return nil, err
	}
	for i := range items {
		lock, lockErr := s.lock(items[i].OperationID)
		if lockErr != nil {
			continue
		}
		if lock.State == items[i].State {
			continue
		}
		if lock.State == StateRolledBack || lock.State == StateRollbackFailed || lock.State == StateAbandoned || lock.State == StateCancelled {
			items[i], err = s.transitionJournal(ctx, items[i], []string{items[i].State}, lock.State, nil)
			if err != nil {
				return items, err
			}
		}
	}
	return items, nil
}
