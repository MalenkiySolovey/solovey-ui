package firewall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	sptest "github.com/MalenkiySolovey/solovey-ui/testsupport/serverprotection"
)

// memoryOperationStore keeps the normal state-machine proof free
// from SQLite/CGO and from every system backend.
type memoryOperationStore struct {
	mu        sync.Mutex
	items     map[string]protectionrepository.OperationLockModel
	byKey     map[string]string
	failState map[string]error
	nextID    uint
}

type memoryFirewallState struct {
	mu   sync.Mutex
	data map[string][]byte
}

type firewallHelperFunc func(context.Context, protectionhelper.Request) (protectionhelper.Response, error)

func (f firewallHelperFunc) Execute(ctx context.Context, request protectionhelper.Request) (protectionhelper.Response, error) {
	return f(ctx, request)
}

type memoryContributionStore struct {
	mu             sync.Mutex
	contributions  map[string]protectionrepository.FirewallContributionModel
	composition    protectionrepository.FirewallCompositionModel
	hasComposition bool
	observation    protectionrepository.FirewallObservationModel
	hasObservation bool
	observationErr error
	transitions    map[string]protectionrepository.FirewallContributionTransitionModel
}

func newMemoryContributionStore() *memoryContributionStore {
	return &memoryContributionStore{contributions: map[string]protectionrepository.FirewallContributionModel{}, transitions: map[string]protectionrepository.FirewallContributionTransitionModel{}}
}
func (s *memoryContributionStore) FirewallAuthority(context.Context) (protectionrepository.FirewallAuthoritySnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := protectionrepository.FirewallAuthoritySnapshot{Composition: s.composition, HasComposition: s.hasComposition, Observation: s.observation, HasObservation: s.hasObservation}
	for _, value := range s.contributions {
		result.Contributions = append(result.Contributions, value)
	}
	sort.Slice(result.Contributions, func(i, j int) bool {
		return result.Contributions[i].ContributionID < result.Contributions[j].ContributionID
	})
	return result, nil
}
func (s *memoryContributionStore) FirewallTransition(_ context.Context, id string) (protectionrepository.FirewallContributionTransitionModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.transitions[id]
	if !ok {
		return value, protectionrepository.ErrRecordNotFound
	}
	return value, nil
}
func (s *memoryContributionStore) CreateFirewallTransition(_ context.Context, value protectionrepository.FirewallContributionTransitionModel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.transitions[value.OperationID]; ok {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	s.transitions[value.OperationID] = value
	return nil
}
func (s *memoryContributionStore) MarkFirewallTransitionMutation(_ context.Context, id string, marker int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.transitions[id]
	if !ok || value.State != "PREPARED" || marker <= 0 {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	value.State = "MUTATING"
	value.MarkerUnixNano = marker
	s.transitions[id] = value
	return nil
}
func (s *memoryContributionStore) MarkFirewallTransitionMutationCompleted(_ context.Context, id string, completed int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.transitions[id]
	if !ok || value.State != "MUTATING" || value.MarkerUnixNano <= 0 || completed <= value.MarkerUnixNano || value.MutationCompletedUnixNano != 0 {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	value.MutationCompletedUnixNano = completed
	s.transitions[id] = value
	return nil
}
func (s *memoryContributionStore) CommitFirewallAuthority(_ context.Context, operationID, expectedComposition, expectedContribution string, replacement *protectionrepository.FirewallContributionModel, composition protectionrepository.FirewallCompositionModel, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	transition, ok := s.transitions[operationID]
	if !ok {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	currentComposition := ""
	if s.hasComposition {
		currentComposition = s.composition.Revision
	}
	if currentComposition != expectedComposition {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	currentContribution := ""
	if value, exists := s.contributions[transition.ContributionID]; exists {
		currentContribution = value.SemanticRevision
	}
	if currentContribution != expectedContribution {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	if replacement == nil {
		delete(s.contributions, transition.ContributionID)
	} else {
		s.contributions[replacement.ContributionID] = *replacement
	}
	if composition.Schema == "" {
		s.composition = protectionrepository.FirewallCompositionModel{}
		s.hasComposition = false
		s.observation = protectionrepository.FirewallObservationModel{Schema: protectionrepository.FirewallObservationSchemaV1, State: FirewallLiveAbsent}
		s.hasObservation = true
	} else {
		composition.State = "ACTIVE"
		s.composition = composition
		s.hasComposition = true
		s.observation = protectionrepository.FirewallObservationModel{Schema: protectionrepository.FirewallObservationSchemaV1, State: FirewallLiveMatching, HasCommittedAuthority: true,
			CommittedCompositionRevision: composition.Revision, ManagedTablePresent: true, CurrentRevision: composition.ManagedPlanRevision, CurrentSemanticSHA256: composition.CandidateSemanticSHA256,
			CurrentTimedMembershipSHA256: composition.CandidateTimedMembershipSHA256, ExpectedTimedMembershipSHA256: composition.CandidateTimedMembershipSHA256}
		s.hasObservation = true
	}
	transition.State = state
	s.transitions[operationID] = transition
	return nil
}
func (s *memoryContributionStore) RecordFirewallTransitionHealth(_ context.Context, id, provider string, generation uint64, revision string, started, completed, expires int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.transitions[id]
	if !ok || value.State != "APPLIED" || value.MutationCompletedUnixNano <= value.MarkerUnixNano || started < value.MutationCompletedUnixNano || completed < started || expires <= completed || provider == "" || generation == 0 || revision == "" {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	value.State = "HEALTH_VERIFIED"
	value.HealthProviderInstance = provider
	value.HealthGeneration = generation
	value.HealthObservationRevision = revision
	value.HealthStartedUnixNano = started
	value.HealthCompletedUnixNano = completed
	value.HealthExpiresUnixNano = expires
	s.transitions[id] = value
	return nil
}

func (s *memoryContributionStore) RecordFirewallObservation(_ context.Context, value protectionrepository.FirewallObservationModel, expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observationErr != nil {
		return s.observationErr
	}
	if expected == "" && s.hasComposition || expected != "" && (!s.hasComposition || s.composition.Revision != expected) {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	if value.State != FirewallLiveAbsent && value.State != FirewallLiveMatching && value.State != FirewallLiveForeign && value.State != FirewallLiveDrifted && value.State != FirewallLiveUnavailable {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	value.Schema = protectionrepository.FirewallObservationSchemaV1
	value.HasCommittedAuthority = expected != ""
	value.CommittedCompositionRevision = expected
	s.observation, s.hasObservation = value, true
	return nil
}

func (s *memoryContributionStore) RetireFirewallAuthorityAfterRuntimeLoss(_ context.Context, operationID string, operationRevision int, expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if operationID == "" || operationRevision <= 0 || !s.hasObservation || s.observation.State != FirewallLiveAbsent || s.observation.ManagedTablePresent {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	if !s.hasComposition {
		if expected != "" || len(s.contributions) != 0 || s.observation.HasCommittedAuthority {
			return protectionrepository.ErrFirewallAuthorityConflict
		}
		return nil
	}
	if expected == "" || s.composition.Revision != expected || !s.observation.HasCommittedAuthority || s.observation.CommittedCompositionRevision != expected {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	s.contributions = map[string]protectionrepository.FirewallContributionModel{}
	s.composition = protectionrepository.FirewallCompositionModel{}
	s.hasComposition = false
	s.observation = protectionrepository.FirewallObservationModel{Schema: protectionrepository.FirewallObservationSchemaV1, State: FirewallLiveAbsent}
	for id, transition := range s.transitions {
		if transition.State == "APPLIED" || transition.State == "HEALTH_VERIFIED" {
			transition.State = "RETIRED_RUNTIME_LOSS"
			s.transitions[id] = transition
		}
	}
	return nil
}
func (s *memoryContributionStore) SetFirewallTransitionState(_ context.Context, id, from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.transitions[id]
	if !ok || value.State != from {
		return protectionrepository.ErrFirewallAuthorityConflict
	}
	value.State = to
	s.transitions[id] = value
	return nil
}

func (s *memoryFirewallState) WriteFirewallState(operationID string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[operationID] = append([]byte(nil), data...)
	return nil
}

func (s *memoryFirewallState) ReadFirewallState(operationID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[operationID]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func newMemoryOperationStore() *memoryOperationStore {
	return &memoryOperationStore{items: map[string]protectionrepository.OperationLockModel{}, byKey: map[string]string{}, failState: map[string]error{}}
}

func (s *memoryOperationStore) AcquireOperationLock(_ context.Context, input protectionrepository.AcquireOperationLockInput) (protectionrepository.OperationLockModel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byKey[input.IdempotencyKey]; ok {
		return s.items[id], true, nil
	}
	for _, item := range s.items {
		if fakeNonTerminal(item.State) {
			return protectionrepository.OperationLockModel{}, false, protectionrepository.ErrOperationConflict
		}
	}
	s.nextID++
	pid := input.LockedByPID
	item := protectionrepository.OperationLockModel{ID: s.nextID, OperationID: input.OperationID, Kind: input.Kind, ResourceID: input.ResourceID, Protocol: input.Protocol, Listen: input.Listen, Port: input.Port, State: input.State, Revision: 1, IdempotencyKey: input.IdempotencyKey, PlanRevision: input.PlanRevision, HelperRevision: input.HelperRevision, LockedByPID: &pid, LockedByInstanceID: input.LockedByInstanceID, Actor: input.Actor, HeartbeatAt: input.Now, ExpiresAt: input.ExpiresAt, CreatedAt: input.Now, UpdatedAt: input.Now}
	s.items[item.OperationID] = item
	s.byKey[input.IdempotencyKey] = item.OperationID
	return item, false, nil
}

func (s *memoryOperationStore) OperationByIdempotencyKey(_ context.Context, key string) (protectionrepository.OperationLockModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byKey[key]
	if !ok {
		return protectionrepository.OperationLockModel{}, protectionrepository.ErrRecordNotFound
	}
	return s.items[id], nil
}

func (s *memoryOperationStore) OperationByID(_ context.Context, id string) (protectionrepository.OperationLockModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return protectionrepository.OperationLockModel{}, protectionrepository.ErrRecordNotFound
	}
	return item, nil
}

func (s *memoryOperationStore) ListOperationLocks(_ context.Context, states []string) ([]protectionrepository.OperationLockModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]protectionrepository.OperationLockModel, 0, len(s.items))
	for _, item := range s.items {
		if len(states) == 0 || fakeContains(states, item.State) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

func (s *memoryOperationStore) UpdateOperationLockFenced(_ context.Context, update protectionrepository.FencedOperationLockUpdate) (protectionrepository.OperationLockModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failState[update.ToState]; err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	item, ok := s.items[update.OperationID]
	if !ok || item.Revision != update.Revision || item.LockedByInstanceID != update.InstanceID || item.LockedByPID == nil || *item.LockedByPID != update.PID || !fakeContains(update.FromStates, item.State) {
		return protectionrepository.OperationLockModel{}, protectionrepository.ErrOperationFenced
	}
	item.State, item.Revision, item.HeartbeatAt, item.ExpiresAt, item.UpdatedAt = update.ToState, update.Revision+1, update.Now, update.ExpiresAt, update.Now
	s.items[item.OperationID] = item
	return item, nil
}

func (s *memoryOperationStore) HeartbeatOperationLock(_ context.Context, id, instance string, pid, revision int, now, expires int64) (protectionrepository.OperationLockModel, error) {
	return s.UpdateOperationLockFenced(context.Background(), protectionrepository.FencedOperationLockUpdate{OperationID: id, Revision: revision, InstanceID: instance, PID: pid, FromStates: protectionrepository.NonTerminalOperationLockStates(), ToState: s.items[id].State, Now: now, ExpiresAt: expires})
}

func (s *memoryOperationStore) RecoverOperationLock(_ context.Context, update protectionrepository.RecoveryOperationLockUpdate) (protectionrepository.OperationLockModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[update.OperationID]
	if !ok || item.Revision != update.ExpectedRevision || !fakeContains(update.FromStates, item.State) {
		return protectionrepository.OperationLockModel{}, protectionrepository.ErrRevisionConflict
	}
	item.State, item.Revision, item.UpdatedAt = update.ToState, item.Revision+1, update.Now
	s.items[item.OperationID] = item
	return item, nil
}

func (s *memoryOperationStore) ReclaimOperationLock(_ context.Context, update protectionrepository.ReclaimOperationLockUpdate) (protectionrepository.OperationLockModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[update.OperationID]
	if !ok || item.Revision != update.Revision || item.State != update.FromState {
		return protectionrepository.OperationLockModel{}, protectionrepository.ErrOperationFenced
	}
	pid := update.PID
	item.State, item.Revision, item.LockedByInstanceID, item.LockedByPID = update.ToState, item.Revision+1, update.InstanceID, &pid
	item.HeartbeatAt, item.ExpiresAt, item.UpdatedAt = update.Now, update.ExpiresAt, update.Now
	s.items[item.OperationID] = item
	return item, nil
}

func (s *memoryOperationStore) ForceUnlockOperation(_ context.Context, id string, revision int, now int64) (protectionrepository.OperationLockModel, error) {
	return s.reviseTerminal(id, revision, "force_unlocked", now)
}
func (s *memoryOperationStore) ForgetOperationState(_ context.Context, id string, revision int, now int64) (protectionrepository.OperationLockModel, error) {
	return s.reviseTerminal(id, revision, "forgotten", now)
}
func (s *memoryOperationStore) reviseTerminal(id string, revision int, state string, now int64) (protectionrepository.OperationLockModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok || item.Revision != revision {
		return protectionrepository.OperationLockModel{}, protectionrepository.ErrRevisionConflict
	}
	item.State, item.Revision, item.UpdatedAt = state, revision+1, now
	s.items[id] = item
	return item, nil
}
func (s *memoryOperationStore) MarkOperationRecovery(_ context.Context, id string, attempts int, at int64, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.items[id]
	item.RecoveryAttempts, item.RecoveryErrorCode, item.UpdatedAt = attempts, code, at
	item.LastRecoveryAt = &at
	s.items[id] = item
	return nil
}

func (s *memoryOperationStore) RecordRuntimeRecoveryFailure(_ context.Context, claimed protectionrepository.OperationLockModel, attempts int, at int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[claimed.OperationID]
	if !ok || item.Revision != claimed.Revision || item.State != claimed.State || item.LockedByInstanceID != claimed.LockedByInstanceID || item.LockedByPID == nil || claimed.LockedByPID == nil || *item.LockedByPID != *claimed.LockedByPID {
		return protectionrepository.ErrOperationFenced
	}
	item.RecoveryAttempts, item.RecoveryErrorCode, item.UpdatedAt = attempts, "runtime_restore_execution_failed", at
	item.LastRecoveryAt = &at
	if attempts >= 2 {
		item.State, item.Revision = "reconcile_required", item.Revision+1
	}
	s.items[item.OperationID] = item
	return nil
}

func fakeContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func fakeNonTerminal(state string) bool {
	return fakeContains(protectionrepository.NonTerminalOperationLockStates(), state)
}

type fakeArtifactService struct{ root string }

func (s fakeArtifactService) WriteRevision(_ context.Context, operationID, revision string, files map[string][]byte) (protectionrepository.ArtifactModel, error) {
	dir := filepath.Join(s.root, "revisions", revision)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return protectionrepository.ArtifactModel{}, err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return protectionrepository.ArtifactModel{}, err
		}
	}
	return protectionrepository.ArtifactModel{OperationID: operationID, Revision: revision, RelativePath: filepath.ToSlash(filepath.Join("revisions", revision))}, nil
}

type fakeMarker struct{}

func (fakeMarker) MarkMutation(string, string) error { return nil }

type fakeDeadPID struct{}

func (fakeDeadPID) Alive(int) (bool, error) { return false, nil }

func TestFakeCIStateMachine(t *testing.T) {
	t.Run("unknown SSH and unproven rate primitive block before helper", func(t *testing.T) {
		workflow, mock, _, _ := newFakeCIWorkflow(t, nil)
		unknown := BuildPlan([]hostresources.ProtectableResource{{ID: "panel", Kind: "panel_web", Protocol: "http", Port: 443}}, nil, nil)
		if _, err := workflow.Prepare(context.Background(), PrepareInput{Plan: unknown, Actor: "ci", IdempotencyKey: "unknown-ssh", Confirmation: "PREPARE SERVER PROTECTION " + unknown.Revision}); !errors.Is(err, ErrUnknownSSH) {
			t.Fatalf("unknown SSH error=%v", err)
		}
		storm := applyPlan()
		storm.StormLimits = []StormLimit{{Protocol: "tcp", Rate: 10, Burst: 20}}
		if _, err := workflow.Prepare(context.Background(), PrepareInput{Plan: storm, Actor: "ci", IdempotencyKey: "storm", Confirmation: "PREPARE SERVER PROTECTION " + storm.Revision}); !errors.Is(err, ErrMissingCapability) {
			t.Fatalf("storm capability error=%v", err)
		}
		if len(mock.Requests) != 0 {
			t.Fatalf("blocked plan reached helper: %#v", mock.Requests)
		}
	})

	t.Run("health failure rolls back", func(t *testing.T) {
		workflow, mock, _, store := newFakeCIWorkflow(t, func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
			return []componenthealth.Result{{ResourceID: "core:panel:web", Status: componenthealth.StatusDegraded, FactCode: "listener_unavailable"}}
		})
		plan := applyPlan()
		prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "health", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
		if err != nil {
			t.Fatal(err)
		}
		result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
		if !errors.Is(err, ErrHealthFailed) || result.State != protectionoperations.StateRolledBack || !result.RollbackAttempted {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		item, _ := store.OperationByID(context.Background(), prepared.Operation.OperationID)
		if item.State != protectionoperations.StateRolledBack || len(mock.Requests) != 10 {
			t.Fatalf("item=%#v requests=%d", item, len(mock.Requests))
		}
	})

	t.Run("empty or incomplete health cannot report applied", func(t *testing.T) {
		for name, health := range map[string]HealthCheck{
			"empty": func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result { return nil },
			"wrong-resource": func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
				return []componenthealth.Result{{ResourceID: "unrelated", Status: componenthealth.StatusOK, FactCode: "listener_ready"}}
			},
		} {
			t.Run(name, func(t *testing.T) {
				workflow, _, _, _ := newFakeCIWorkflow(t, health)
				plan := applyPlan()
				_, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "health-fail-closed-" + name, Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
				if !errors.Is(err, ErrMissingCapability) {
					t.Fatalf("missing health producer was not rejected: %v", err)
				}
			})
		}
	})

	t.Run("verify and rollback failures are terminal", func(t *testing.T) {
		workflow, mock, _, store := newFakeCIWorkflow(t, nil)
		bundles := 0
		workflow.Recovery = MockRecovery{Bundle: func(context.Context, protectionrepository.OperationLockModel, string) error { bundles++; return nil }}
		plan := applyPlan()
		prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "verify", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
		if err != nil {
			t.Fatal(err)
		}
		mock.Responses[protectionhelper.OperationNFTApply] = protectionhelper.Response{OK: true, NFT: &protectionhelper.NFTResult{AppliedRevision: "stale"}}
		mock.Responses[protectionhelper.OperationNFTRollback] = protectionhelper.Response{OK: false, Code: protectionhelper.CodeTransportFailed}
		result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
		if err == nil || result.State != protectionoperations.StateRollbackFailed || bundles != 1 {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		item, _ := store.OperationByID(context.Background(), prepared.Operation.OperationID)
		if item.State != protectionoperations.StateRollbackFailed {
			t.Fatalf("item=%#v", item)
		}
	})

	t.Run("bundle failure cannot publish rollback_failed", func(t *testing.T) {
		workflow, mock, _, store := newFakeCIWorkflow(t, nil)
		workflow.Recovery = MockRecovery{Bundle: func(context.Context, protectionrepository.OperationLockModel, string) error {
			return errors.New("injected bundle failure")
		}}
		plan := applyPlan()
		prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "bundle-failure", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
		if err != nil {
			t.Fatal(err)
		}
		mock.FailAfter[protectionhelper.OperationNFTApply] = errors.New("injected post-mutation apply observation failure")
		mock.Responses[protectionhelper.OperationNFTRollback] = protectionhelper.Response{OK: false, Code: protectionhelper.CodeTransportFailed}
		result, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
		item, loadErr := store.OperationByID(context.Background(), prepared.Operation.OperationID)
		if err == nil || loadErr != nil || result.State != protectionoperations.StateRollingBack || item.State != protectionoperations.StateRollingBack {
			t.Fatalf("bundle failure published terminal state: result=%#v item=%#v err=%v load=%v", result, item, err, loadErr)
		}
	})

	t.Run("helper capability revision is fenced before artifacts", func(t *testing.T) {
		workflow, mock, _, _ := newFakeCIWorkflow(t, nil)
		plan := applyPlan()
		prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "helper-revision", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
		if err != nil {
			t.Fatal(err)
		}
		requestsBefore := len(mock.Requests)
		mock.Capabilities.NFT.Version = "changed-after-prepare"
		if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID}); !errors.Is(err, ErrHelperRevision) {
			t.Fatalf("helper revision error=%v", err)
		}
		if len(mock.Requests) != requestsBefore+1 {
			t.Fatalf("helper revision conflict reached mutation: %#v", mock.Requests[requestsBefore:])
		}
	})

	t.Run("applied retry is idempotent and stale plan is fenced", func(t *testing.T) {
		workflow, mock, _, _ := newFakeCIWorkflow(t, nil)
		plan := applyPlan()
		prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "retry", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
		if err != nil {
			t.Fatal(err)
		}
		requestsAfterPrepare := len(mock.Requests)
		if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Confirmation: "wrong"}); !errors.Is(err, protectionoperations.ErrConfirmationRequired) {
			t.Fatalf("confirmation error=%v", err)
		}
		if len(mock.Requests) != requestsAfterPrepare {
			t.Fatalf("unconfirmed apply reached helper: %#v", mock.Requests)
		}
		stale := plan
		stale.Revision = hostresources.Revision("stale")
		if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: stale, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID}); !errors.Is(err, ErrPlanRevision) {
			t.Fatalf("stale error=%v", err)
		}
		first, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
		if err != nil || first.State != protectionoperations.StateApplied {
			t.Fatalf("first=%#v err=%v", first, err)
		}
		requestCount := len(mock.Requests)
		second, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
		if err != nil || second.State != protectionoperations.StateApplied || second.ActualStatus != "APPLIED" || len(mock.Requests) != requestCount+2 || mock.Requests[requestCount].Operation != protectionhelper.OperationCapabilities || mock.Requests[requestCount+1].Operation != protectionhelper.OperationNFTObserve {
			t.Fatalf("second=%#v err=%v requests=%d/%d", second, err, requestCount, len(mock.Requests))
		}
	})
}

func TestFakeCIRestartRecoveryNeverRepeatsApply(t *testing.T) {
	workflow, mock, manager, store := newFakeCIWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "restart", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	applying, err := manager.Transition(context.Background(), prepared.Operation.OperationID, prepared.Operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	rollbacks := 0
	restarted := protectionoperations.NewManager(store, protectionoperations.Options{InstanceID: "restart-ci", PID: 99, Now: func() time.Time { return time.Unix(applying.ExpiresAt+1, 0) }, PIDProbe: fakeDeadPID{}, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }, Recovery: MockRecovery{Rollback: func(context.Context, protectionrepository.OperationLockModel) error { rollbacks++; return nil }}})
	results, err := restarted.Recover(context.Background())
	if err != nil || rollbacks != 1 || len(results) != 2 || results[1].ToState != protectionoperations.StateRolledBack {
		t.Fatalf("results=%#v rollbacks=%d err=%v", results, rollbacks, err)
	}
	for _, request := range mock.Requests {
		if request.Operation == protectionhelper.OperationNFTApply {
			t.Fatal("restart recovery repeated apply")
		}
	}
}

func TestRuntimeLossReconcilerRetiresVolatileAuthorityAndAllowsNextApply(t *testing.T) {
	workflow, mock, manager, store := newFakeCIWorkflow(t, nil)
	plan := applyPlan()
	applied := applyCompositionWorkflowPlan(t, &workflow, plan, "runtime-loss-before-reboot")
	applyCalls := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply)
	if err := manager.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Model a reboot: SQLite-backed authority survives, while /run artifacts and
	// the kernel table do not. Startup must classify that exact absence and
	// retire the stale rollback promise without replaying the candidate.
	rootPath := workflow.Artifacts.(fakeArtifactService).root
	if err := os.RemoveAll(rootPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	state := workflow.State.(*memoryFirewallState)
	state.mu.Lock()
	state.data = map[string][]byte{}
	state.mu.Unlock()
	mock.ManagedTablePresent, mock.ManagedPlanRevision, mock.ManagedCandidateSHA, mock.ManagedCandidateSemantic, mock.ManagedTimedMembership = false, "", "", "", ""

	restarted := protectionoperations.NewManager(store, protectionoperations.Options{
		InstanceID: "runtime-loss-restart", PID: 88,
		Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil },
	})
	t.Cleanup(func() { _ = restarted.Stop(context.Background()) })
	client, err := protectionhelper.NewClient(sptest.ManagedRoot(t, rootPath), restarted, mock, &helperAudit{})
	if err != nil {
		t.Fatal(err)
	}
	workflow.Manager, workflow.Helper = restarted, client
	if err := restarted.SetReconcilerForKind(protectionoperations.KindFirewall, RuntimeLossReconciler{Workflow: &workflow}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	retired, err := store.OperationByID(t.Context(), applied.OperationID)
	if err != nil || retired.State != protectionoperations.StateForgotten {
		t.Fatalf("lost volatile authority was not terminalized: operation=%#v err=%v", retired, err)
	}
	snapshot, err := workflow.Contributions.FirewallAuthority(t.Context())
	if err != nil || snapshot.HasComposition || len(snapshot.Contributions) != 0 || !snapshot.HasObservation || snapshot.Observation.State != FirewallLiveAbsent || snapshot.Observation.HasCommittedAuthority {
		t.Fatalf("lost volatile authority remained durable: snapshot=%#v err=%v", snapshot, err)
	}
	if got := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply); got != applyCalls {
		t.Fatalf("startup reconciliation replayed a candidate: apply calls %d -> %d", applyCalls, got)
	}
	rollbackCalls := helperOperationCount(mock.Requests, protectionhelper.OperationNFTRollback)
	if result, rollbackErr := workflow.Rollback(t.Context(), applied.OperationID, "ROLLBACK SERVER PROTECTION "+applied.OperationID); rollbackErr == nil || result.RollbackAttempted || helperOperationCount(mock.Requests, protectionhelper.OperationNFTRollback) != rollbackCalls {
		t.Fatalf("retired operation still presented consumable rollback: result=%#v err=%v", result, rollbackErr)
	}

	nextPlan := compositionBaselineWithTCPPort(plan, 8443)
	next := applyCompositionWorkflowPlan(t, &workflow, nextPlan, "runtime-loss-after-reboot")
	if next.State != protectionoperations.StateApplied || helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply) != applyCalls+1 {
		t.Fatalf("fresh post-reboot apply did not establish new authority: operation=%#v", next)
	}
}

func TestLostFirewallEventFallbackConvergesToAbsentWithoutReplay(t *testing.T) {
	workflow, mock, _, _ := newFakeCIWorkflow(t, nil)
	applyCompositionWorkflowPlan(t, &workflow, applyPlan(), "lost-firewall-event")
	applyCalls := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply)
	mock.ManagedTablePresent, mock.ManagedPlanRevision, mock.ManagedCandidateSHA, mock.ManagedCandidateSemantic, mock.ManagedTimedMembership = false, "", "", "", ""

	// This is the exact callback used by the bounded component schedule when a
	// platform event is lost. It performs one observation and no mutation.
	observation, err := workflow.ReconcileAuthority(t.Context())
	if err != nil || observation.State != FirewallLiveAbsent || !observation.Persisted {
		t.Fatalf("fallback observation did not converge: observation=%#v err=%v", observation, err)
	}
	if got := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply); got != applyCalls {
		t.Fatalf("fallback replayed a candidate: apply calls %d -> %d", applyCalls, got)
	}
}

func TestFirewallAuthorityReconciliationMatrixNeverReplaysCandidate(t *testing.T) {
	t.Run("inactive absent and uncommitted live table", func(t *testing.T) {
		workflow, mock, _, _ := newFakeCIWorkflow(t, nil)
		applyCalls := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply)
		observation, err := workflow.ReconcileAuthority(t.Context())
		if err != nil || observation.State != FirewallLiveAbsent || observation.ManagedTablePresent || !observation.Persisted {
			t.Fatalf("inactive/absent observation=%#v err=%v", observation, err)
		}
		snapshot, loadErr := workflow.Contributions.FirewallAuthority(t.Context())
		if loadErr != nil || snapshot.HasComposition || !snapshot.HasObservation || snapshot.Observation.State != FirewallLiveAbsent || snapshot.Observation.HasCommittedAuthority {
			t.Fatalf("inactive/absent was not durably separated: snapshot=%#v err=%v", snapshot, loadErr)
		}

		mock.ManagedTablePresent = true
		mock.ManagedPlanRevision = strings.Repeat("a", 64)
		mock.ManagedCandidateSemantic = strings.Repeat("b", 64)
		mock.ManagedTimedMembership = strings.Repeat("c", 64)
		observation, err = workflow.ReconcileAuthority(t.Context())
		if err != nil || observation.State != FirewallLiveForeign || observation.Reason != "uncommitted_managed_table" || !observation.Persisted {
			t.Fatalf("inactive/live table was silently adopted: observation=%#v err=%v", observation, err)
		}
		snapshot, _ = workflow.Contributions.FirewallAuthority(t.Context())
		if !snapshot.HasObservation || snapshot.Observation.State != FirewallLiveForeign || snapshot.Observation.HasCommittedAuthority || !snapshot.Observation.ManagedTablePresent {
			t.Fatalf("inactive/live table was not durably classified: %#v", snapshot.Observation)
		}
		if got := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply); got != applyCalls {
			t.Fatalf("reconciliation replayed a candidate: apply calls %d -> %d", applyCalls, got)
		}
	})

	t.Run("active matching absent drift foreign and unavailable", func(t *testing.T) {
		workflow, mock, _, _ := newFakeCIWorkflow(t, nil)
		applyCompositionWorkflowPlan(t, &workflow, applyPlan(), "reconcile-authority")
		snapshot, err := workflow.Contributions.FirewallAuthority(t.Context())
		if err != nil || !snapshot.HasComposition {
			t.Fatalf("committed authority unavailable: %#v err=%v", snapshot, err)
		}
		applyCalls := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply)
		assertObservation := func(want string, wantErr bool) AuthorityObservation {
			t.Helper()
			observation, reconcileErr := workflow.ReconcileAuthority(t.Context())
			if (reconcileErr != nil) != wantErr || observation.State != want || !observation.Persisted {
				t.Fatalf("state=%s observation=%#v err=%v", want, observation, reconcileErr)
			}
			current, loadErr := workflow.Contributions.FirewallAuthority(t.Context())
			if loadErr != nil || !current.HasObservation || current.Observation.State != want || current.Observation.CommittedCompositionRevision != snapshot.Composition.Revision {
				t.Fatalf("state=%s was not durable/bound: snapshot=%#v err=%v", want, current, loadErr)
			}
			if got := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply); got != applyCalls {
				t.Fatalf("state=%s replayed candidate: apply calls %d -> %d", want, applyCalls, got)
			}
			return observation
		}

		mock.Responses[protectionhelper.OperationNFTObserve] = protectionhelper.Response{OK: true}
		matching := assertObservation(FirewallLiveMatching, false)
		if matching.CurrentRevision != snapshot.Composition.ManagedPlanRevision || matching.CurrentSemanticSHA != snapshot.Composition.CandidateSemanticSHA256 {
			t.Fatalf("matching observation lost exact live identity: %#v", matching)
		}

		mock.ManagedTablePresent, mock.ManagedPlanRevision, mock.ManagedCandidateSemantic, mock.ManagedTimedMembership = false, "", "", ""
		assertObservation(FirewallLiveAbsent, false)

		mock.ManagedTablePresent = true
		mock.ManagedPlanRevision = snapshot.Composition.ManagedPlanRevision
		mock.ManagedCandidateSemantic = strings.Repeat("d", 64)
		mock.ManagedTimedMembership = snapshot.Composition.CandidateTimedMembershipSHA256
		assertObservation(FirewallLiveDrifted, false)

		mock.Responses[protectionhelper.OperationNFTObserve] = protectionhelper.Response{OK: false, Code: protectionhelper.CodeValidationFailed, Reason: "managed_table_semantic_observation_failed"}
		assertObservation(FirewallLiveForeign, true)

		mock.Responses[protectionhelper.OperationNFTObserve] = protectionhelper.Response{OK: false, Code: protectionhelper.CodeMissingCapability, Reason: "nft_access_unavailable"}
		unavailable := assertObservation(FirewallLiveUnavailable, true)
		if unavailable.ManagedTablePresent {
			t.Fatalf("helper loss was misclassified as observed presence: %#v", unavailable)
		}

		store := workflow.Contributions.(*memoryContributionStore)
		store.observationErr = errors.New("observation persistence unavailable")
		failed, persistErr := workflow.ReconcileAuthority(t.Context())
		if persistErr == nil || failed.Persisted {
			t.Fatalf("failed durable observation was presented as published: observation=%#v err=%v", failed, persistErr)
		}
	})
}

func TestRecordWorkflowUnavailablePersistsPreInvocationReason(t *testing.T) {
	store := newMemoryContributionStore()
	observation, err := RecordWorkflowUnavailable(t.Context(), store)
	if err != nil || observation.State != FirewallLiveUnavailable || observation.Reason != FirewallReasonWorkflowUnavailable || !observation.Persisted {
		t.Fatalf("pre-invocation workflow failure lost its reason boundary: observation=%#v err=%v", observation, err)
	}
	snapshot, loadErr := store.FirewallAuthority(t.Context())
	if loadErr != nil || !snapshot.HasObservation || snapshot.Observation.State != FirewallLiveUnavailable || snapshot.Observation.Reason != FirewallReasonWorkflowUnavailable {
		t.Fatalf("pre-invocation workflow failure was not durable: snapshot=%#v err=%v", snapshot, loadErr)
	}
}

func TestReconcileAuthorityPersistsHelperCallFailedAfterInvocation(t *testing.T) {
	workflow, mock, _, _ := newFakeCIWorkflow(t, nil)
	applyCalls := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply)
	helperFailure := errors.New("injected helper transport failure")
	helperCalls := 0
	workflow.Helper = firewallHelperFunc(func(_ context.Context, request protectionhelper.Request) (protectionhelper.Response, error) {
		helperCalls++
		if request.Operation != protectionhelper.OperationNFTObserve {
			t.Fatalf("unexpected helper operation: %s", request.Operation)
		}
		return protectionhelper.Response{}, helperFailure
	})

	observation, callErr := workflow.ReconcileAuthority(t.Context())
	if helperCalls != 1 {
		t.Fatalf("Helper.Execute calls=%d, want 1", helperCalls)
	}
	if !errors.Is(callErr, helperFailure) || observation.State != FirewallLiveUnavailable || observation.Reason != FirewallReasonHelperCallFailed || !observation.Persisted {
		t.Fatalf("helper invocation failure lost its reason boundary: observation=%#v err=%v", observation, callErr)
	}
	snapshot, loadErr := workflow.Contributions.FirewallAuthority(t.Context())
	if loadErr != nil || !snapshot.HasObservation || snapshot.Observation.Reason != FirewallReasonHelperCallFailed {
		t.Fatalf("helper invocation failure was not durable: snapshot=%#v err=%v", snapshot, loadErr)
	}
	if got := helperOperationCount(mock.Requests, protectionhelper.OperationNFTApply); got != applyCalls {
		t.Fatalf("helper invocation failure replayed a candidate: apply calls %d -> %d", applyCalls, got)
	}
}

func TestFakeCIRestartBundleFailureCannotPublishRollbackFailed(t *testing.T) {
	workflow, _, manager, store := newFakeCIWorkflow(t, nil)
	plan := applyPlan()
	prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "restart-bundle", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	applying, err := manager.Transition(context.Background(), prepared.Operation.OperationID, prepared.Operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := protectionoperations.NewManager(store, protectionoperations.Options{
		InstanceID: "restart-bundle-ci", PID: 101, Now: func() time.Time { return time.Unix(applying.ExpiresAt+1, 0) }, PIDProbe: fakeDeadPID{},
		Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil },
		Recovery: MockRecovery{
			HasArtifact: func(context.Context, protectionrepository.OperationLockModel) (bool, error) { return true, nil },
			Rollback: func(context.Context, protectionrepository.OperationLockModel) error {
				return errors.New("injected rollback failure")
			},
			Bundle: func(context.Context, protectionrepository.OperationLockModel, string) error {
				return errors.New("injected bundle failure")
			},
		},
	})
	t.Cleanup(func() { _ = restarted.Stop(context.Background()) })
	results, err := restarted.Recover(context.Background())
	item, loadErr := store.OperationByID(context.Background(), applying.OperationID)
	if err != nil || loadErr != nil || len(results) != 2 || results[1].ToState != protectionoperations.StateLockSuspect || item.State != protectionoperations.StateLockSuspect {
		t.Fatalf("bundle failure published terminal state: results=%#v item=%#v err=%v load=%v", results, item, err, loadErr)
	}
}

func TestFakeCITransitionFailureInjection(t *testing.T) {
	for _, fixture := range []struct {
		name, failedTransition, expectedState                    string
		healthFailure, helperApplyFailure, helperRollbackFailure bool
	}{
		{"enter applying", protectionoperations.StateApplying, protectionoperations.StatePrepared, false, false, false},
		{"mark applied", protectionoperations.StateApplied, protectionoperations.StateApplying, false, false, false},
		{"record health failure", protectionoperations.StateHealthFailed, protectionoperations.StateApplying, true, false, false},
		{"enter rollback", protectionoperations.StateRollingBack, protectionoperations.StateHealthFailed, true, false, false},
		{"mark rolled back", protectionoperations.StateRolledBack, protectionoperations.StateRollingBack, true, false, false},
		{"record rollback failure", protectionoperations.StateRollbackFailed, protectionoperations.StateRollingBack, false, true, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var health HealthCheck
			if fixture.healthFailure {
				health = func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
					return []componenthealth.Result{{ResourceID: "core:panel:web", Status: componenthealth.StatusDegraded, FactCode: "injected_health_failure"}}
				}
			}
			workflow, mock, _, store := newFakeCIWorkflow(t, health)
			plan := applyPlan()
			prepared, err := workflow.Prepare(context.Background(), PrepareInput{Plan: plan, Actor: "ci", IdempotencyKey: "transition-" + fixture.failedTransition, Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
			if err != nil {
				t.Fatal(err)
			}
			store.failState[fixture.failedTransition] = errors.New("injected transition failure")
			if fixture.helperApplyFailure {
				mock.FailAfter[protectionhelper.OperationNFTApply] = errors.New("injected post-mutation apply observation failure")
			}
			if fixture.helperRollbackFailure {
				mock.Responses[protectionhelper.OperationNFTRollback] = protectionhelper.Response{OK: false, Code: protectionhelper.CodeTransportFailed}
			}
			if _, err := workflow.Apply(context.Background(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID}); err == nil {
				t.Fatal("injected transition failure reported success")
			}
			item, err := store.OperationByID(context.Background(), prepared.Operation.OperationID)
			if err != nil || item.State != fixture.expectedState {
				t.Fatalf("state=%#v err=%v", item, err)
			}
		})
	}
}

func newFakeCIWorkflow(t *testing.T, health HealthCheck) (Workflow, *testHelperInvoker, *protectionoperations.Manager, *memoryOperationStore) {
	t.Helper()
	store := newMemoryOperationStore()
	manager := protectionoperations.NewManager(store, protectionoperations.Options{InstanceID: "fake-ci", PID: 77, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	rootPath := filepath.Join(t.TempDir(), ".runtime", "server-protection")
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	root := sptest.ManagedRoot(t, rootPath)
	capabilities := protectionhelper.DefaultCapabilities()
	for i := range capabilities.Capabilities {
		switch capabilities.Capabilities[i].Operation {
		case protectionhelper.OperationNFTValidate, protectionhelper.OperationNFTObserve, protectionhelper.OperationNFTApply, protectionhelper.OperationNFTRollback:
			capabilities.Capabilities[i].Available = true
			capabilities.Capabilities[i].Reason = ""
		}
	}
	capabilities.NFT = protectionhelper.NFTSupport{PlatformKnown: true, Linux: true, Available: true, TTLSet: true, RateLimit: true}
	mock := newTestHelperInvoker(capabilities)
	for _, operation := range []protectionhelper.Operation{protectionhelper.OperationNFTValidate, protectionhelper.OperationNFTObserve, protectionhelper.OperationNFTApply, protectionhelper.OperationNFTRollback} {
		mock.Responses[operation] = protectionhelper.Response{OK: true}
	}
	client, err := protectionhelper.NewClient(root, manager, mock, &helperAudit{})
	if err != nil {
		t.Fatal(err)
	}
	if health == nil {
		health = passingHealth
	}
	state := &memoryFirewallState{data: map[string][]byte{}}
	contributions := newMemoryContributionStore()
	return Workflow{
		Manager: manager, Helper: client, Artifacts: fakeArtifactService{root: rootPath}, Marker: fakeMarker{}, State: state, Recovery: MockRecovery{}, Health: health,
		RollbackHealth: func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
			return []componenthealth.Result{{ResourceID: "rollback:panel", Status: componenthealth.StatusOK, FactCode: "listener_ready"}}
		},
		Contributions: contributions, Now: func() time.Time { return time.Unix(1000, 0).UTC() },
	}, mock, manager, store
}
