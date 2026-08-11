package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func (s *Service) transitionJournal(ctx context.Context, item protectionrepository.PortOperationModel, from []string, state string, health []byte) (protectionrepository.PortOperationModel, error) {
	return s.Journal.UpdatePortOperationFenced(ctx, protectionrepository.FencedPortOperationUpdate{OperationID: item.OperationID, Revision: item.Revision, FromStates: from, ToState: state, HealthJSON: health, UpdatedAt: s.now().Unix()})
}
func (s *Service) lock(operationID string) (protectionrepository.OperationLockModel, error) {
	items, err := s.Operations.List(context.Background())
	if err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	for _, item := range items {
		if item.OperationID == operationID {
			return item, nil
		}
	}
	return protectionrepository.OperationLockModel{}, protectionrepository.ErrRecordNotFound
}
func (s *Service) cancelledBeforeBoundary(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled[id]
}
func (s *Service) cancelledAfterBoundary(id string) bool { return s.cancelledBeforeBoundary(id) }
func (s *Service) ready() error {
	if s == nil || s.Journal == nil || s.Operations == nil || s.Inbound == nil || s.Restart == nil || s.Ownership == nil || s.Fallback == nil || s.Health == nil || s.Helper == nil || s.Snapshot == nil || s.Listener == nil || s.Recovery == nil {
		return ErrServiceDisabled
	}
	if s.initErr != nil {
		return errors.Join(ErrServiceDisabled, s.initErr)
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	return nil
}

func (s *Service) verifyRestored(ctx context.Context, previous OwnerSnapshot, fence Fence) error {
	if err := s.verifyExactOwner(ctx, previous, fence); err != nil {
		return errors.Join(ErrRollbackVerify, err)
	}
	results, err := s.Health.Check(ctx, []HealthTarget{{ResourceID: previous.ResourceID, Check: "rollback_previous_owner"}})
	results = safeHealthResults(results)
	if err != nil || !healthyFor([]HealthTarget{{ResourceID: previous.ResourceID, Check: "rollback_previous_owner"}}, results) {
		return errors.Join(ErrRollbackVerify, err)
	}
	return nil
}

func (s *Service) verifyExactOwner(ctx context.Context, expected OwnerSnapshot, fence Fence) error {
	_, fence, err := s.currentFence(ctx, fence.OperationID, StateApplying, StateHealth, StateRollingBack)
	if err != nil {
		return err
	}
	if err := s.Listener.Verify(ctx, expected, fence); err != nil {
		return err
	}
	current, err := s.Ownership.Current(ctx, expected.Protocol, expected.Listen, expected.Port)
	if err != nil || current.ResourceID != expected.ResourceID || current.Owner != expected.Owner || current.Kind != expected.Kind || !strings.EqualFold(current.Protocol, expected.Protocol) || canonicalListen(current.Listen) != canonicalListen(expected.Listen) || current.Port != expected.Port || current.ProxyProtocol != expected.ProxyProtocol || current.ResourceRevision != expected.ResourceRevision || current.ConfigRevision != expected.ConfigRevision || current.Fingerprint != expected.Fingerprint {
		return errors.Join(ErrListenerVerify, err)
	}
	return nil
}

func (s *Service) validateFence(ctx context.Context, fence Fence) error {
	return s.Operations.ValidateHelperLock(ctx, fence.OperationID, fence.InstanceID, protectionoperations.KindPortHandoff, fence.Revision)
}

func (s *Service) currentFence(ctx context.Context, operationID string, states ...string) (protectionrepository.OperationLockModel, Fence, error) {
	lock, err := s.lock(operationID)
	if err != nil {
		return protectionrepository.OperationLockModel{}, Fence{}, err
	}
	allowed := len(states) == 0
	for _, state := range states {
		allowed = allowed || lock.State == state
	}
	if !allowed {
		return lock, Fence{}, protectionoperations.ErrFenced
	}
	fence := fenceFromLock(lock, s.Operations.InstanceID())
	if err := s.validateFence(ctx, fence); err != nil {
		return lock, Fence{}, err
	}
	return lock, fence, nil
}

func fenceFromLock(lock protectionrepository.OperationLockModel, instanceID string) Fence {
	pid := 0
	if lock.LockedByPID != nil {
		pid = *lock.LockedByPID
	}
	return Fence{OperationID: lock.OperationID, Revision: lock.Revision, InstanceID: instanceID, PID: pid}
}

func (s *Service) abortPrepared(ctx context.Context, next, previous OwnerSnapshot, fence Fence) error {
	abortCtx, cancel := detachedRollbackContext(ctx)
	defer cancel()
	return errors.Join(s.Inbound.AbortPrepare(abortCtx, next, fence), s.Fallback.AbortPrepare(abortCtx, previous, fence))
}

func detachedRollbackContext(context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

func (s *Service) clearCancelled(id string) {
	s.mu.Lock()
	delete(s.cancelled, id)
	s.mu.Unlock()
}
func (s *Service) now() time.Time { return s.Now() }

func snapshots(item protectionrepository.PortOperationModel) (OwnerSnapshot, OwnerSnapshot, error) {
	var a, b OwnerSnapshot
	if err := json.Unmarshal(item.PreviousResourceJSON, &a); err != nil {
		return a, b, err
	}
	if err := json.Unmarshal(item.NextResourceJSON, &b); err != nil {
		return a, b, err
	}
	return a, b, nil
}
func cloneSnapshot(v OwnerSnapshot) OwnerSnapshot {
	out := v
	out.ReservedRoutes = append([]string(nil), v.ReservedRoutes...)
	if v.Profile.ALPNFallbacks != nil {
		out.Profile.ALPNFallbacks = map[string]string{}
		for k, value := range v.Profile.ALPNFallbacks {
			out.Profile.ALPNFallbacks[k] = value
		}
	}
	return out
}
func intPtr(v int) *int { return &v }
func healthTargets(previous, next OwnerSnapshot) []HealthTarget {
	result := []HealthTarget{{ResourceID: previous.ResourceID, Check: "previous_owner"}, {ResourceID: next.ResourceID, Check: "next_owner"}}
	sort.Slice(result, func(i, j int) bool { return result[i].ResourceID < result[j].ResourceID })
	return result
}
func journalHealthTargets(item protectionrepository.PortOperationModel, previous, next OwnerSnapshot) []HealthTarget {
	targets := healthTargets(previous, next)
	var extra []HealthTarget
	if len(item.HealthTargetsJSON) > 0 && json.Unmarshal(item.HealthTargetsJSON, &extra) == nil {
		seen := map[string]bool{}
		for _, target := range targets {
			seen[target.ResourceID+"\x00"+target.Check] = true
		}
		for _, target := range extra {
			key := target.ResourceID + "\x00" + target.Check
			if strings.TrimSpace(target.ResourceID) != "" && strings.TrimSpace(target.Check) != "" && !seen[key] {
				seen[key] = true
				targets = append(targets, target)
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].ResourceID+targets[i].Check < targets[j].ResourceID+targets[j].Check
	})
	return targets
}
func recoveryFacts(lock protectionrepository.OperationLockModel, previous, next OwnerSnapshot) RecoveryFacts {
	return RecoveryFacts{OperationID: lock.OperationID, State: StateRollbackFailed, ResourceID: previous.ResourceID, Protocol: previous.Protocol, Listen: previous.Listen, Port: previous.Port, FromOwner: previous.Owner, ToOwner: next.Owner}
}
func healthyFor(targets []HealthTarget, results []HealthResult) bool {
	if len(results) != len(targets) || len(results) == 0 {
		return false
	}
	expected := make(map[string]bool, len(targets))
	for _, target := range targets {
		expected[target.ResourceID+"\x00"+target.Check] = true
	}
	for _, result := range results {
		key := result.Target.ResourceID + "\x00" + result.Target.Check
		if !result.OK || !expected[key] {
			return false
		}
		delete(expected, key)
	}
	return len(expected) == 0
}

func safeHealthResults(results []HealthResult) []HealthResult {
	out := append([]HealthResult(nil), results...)
	for i := range out {
		fact := strings.TrimSpace(out[i].Fact)
		if fact == "" || len(fact) > 64 {
			out[i].Fact = "health_fact_redacted"
			continue
		}
		for _, r := range fact {
			if !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				out[i].Fact = "health_fact_redacted"
				break
			}
		}
	}
	return out
}
