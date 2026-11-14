// Package backendstore projects typed protection operations onto the existing
// Backend repository. It owns no second schema, connection or transaction.
package backendstore

import (
	"context"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
	"net/netip"
	"time"
)

type Store struct{ repository *repository.Repository }

func New(repository *repository.Repository) *Store {
	if repository == nil {
		return nil
	}
	return &Store{repository: repository}
}

func projectSlice[S, T any](values []S, project func(S) T) []T {
	if values == nil {
		return nil
	}
	result := make([]T, len(values))
	for i, value := range values {
		result[i] = project(value)
	}
	return result
}

func fromArtifactModel(value repository.ArtifactModel) durable.ArtifactModel {
	return durable.ArtifactModel(value)
}
func fromFirewallAuthoritySnapshot(value repository.FirewallAuthoritySnapshot) durable.FirewallAuthoritySnapshot {
	return durable.FirewallAuthoritySnapshot{Contributions: projectSlice(value.Contributions, fromFirewallContributionModel), Composition: fromFirewallCompositionModel(value.Composition), HasComposition: value.HasComposition, Observation: durable.FirewallObservationModel(value.Observation), HasObservation: value.HasObservation}
}
func toFirewallCompositionModel(value durable.FirewallCompositionModel) repository.FirewallCompositionModel {
	return repository.FirewallCompositionModel{Runtime: repository.FirewallRuntimeBinding(value.Runtime), ID: value.ID, Schema: value.Schema, Revision: value.Revision, ManagedPlanRevision: value.ManagedPlanRevision, CandidateSHA256: value.CandidateSHA256, CandidateSemanticSHA256: value.CandidateSemanticSHA256, CandidateTimedMembershipSHA256: value.CandidateTimedMembershipSHA256, BindingsJSON: value.BindingsJSON, State: value.State, AppliedOperationID: value.AppliedOperationID, UpdatedAt: value.UpdatedAt}
}
func fromFirewallCompositionModel(value repository.FirewallCompositionModel) durable.FirewallCompositionModel {
	return durable.FirewallCompositionModel{Runtime: durable.FirewallRuntimeBinding(value.Runtime), ID: value.ID, Schema: value.Schema, Revision: value.Revision, ManagedPlanRevision: value.ManagedPlanRevision, CandidateSHA256: value.CandidateSHA256, CandidateSemanticSHA256: value.CandidateSemanticSHA256, CandidateTimedMembershipSHA256: value.CandidateTimedMembershipSHA256, BindingsJSON: value.BindingsJSON, State: value.State, AppliedOperationID: value.AppliedOperationID, UpdatedAt: value.UpdatedAt}
}
func fromFirewallContributionModel(value repository.FirewallContributionModel) durable.FirewallContributionModel {
	return durable.FirewallContributionModel(value)
}
func fromFirewallContributionTransitionModel(value repository.FirewallContributionTransitionModel) durable.FirewallContributionTransitionModel {
	return durable.FirewallContributionTransitionModel(value)
}
func fromFrontingStateV2Model(value repository.FrontingStateV2Model) durable.FrontingStateV2Model {
	return durable.FrontingStateV2Model(value)
}
func fromIPAllowlistModel(value repository.IPAllowlistModel) durable.IPAllowlistModel {
	return durable.IPAllowlistModel(value)
}
func fromLocalProxyStateV1Model(value repository.LocalProxyStateV1Model) durable.LocalProxyStateV1Model {
	return durable.LocalProxyStateV1Model(value)
}
func fromNativeFallbackOperationModel(value repository.NativeFallbackOperationModel) durable.NativeFallbackOperationModel {
	return durable.NativeFallbackOperationModel(value)
}
func fromOperationLockModel(value repository.OperationLockModel) durable.OperationLockModel {
	return durable.OperationLockModel(value)
}
func fromUDPGuardStateV1Model(value repository.UDPGuardStateV1Model) durable.UDPGuardStateV1Model {
	return durable.UDPGuardStateV1Model(value)
}

func (s *Store) AcquireOperationLock(ctx context.Context, input durable.AcquireOperationLockInput) (durable.OperationLockModel, bool, error) {
	r0, r1, r2 := s.repository.AcquireOperationLock(ctx, repository.AcquireOperationLockInput(input))
	return durable.OperationLockModel(r0), r1, backendError(r2)
}

func (s *Store) ActiveIPAllowlist(ctx context.Context, now time.Time) ([]netip.Prefix, error) {
	r0, r1 := s.repository.ActiveIPAllowlist(ctx, now)
	return r0, backendError(r1)
}

func (s *Store) AdvanceNativeFallbackOperation(ctx context.Context, update durable.NativeFallbackJournalUpdate) (durable.NativeFallbackOperationModel, error) {
	r0, r1 := s.repository.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate(update))
	return durable.NativeFallbackOperationModel(r0), backendError(r1)
}

func (s *Store) AmbiguousFrontingReceiptV2(ctx context.Context, action, key, digest string, now int64) error {
	r0 := s.repository.AmbiguousFrontingReceiptV2(ctx, action, key, digest, now)
	return backendError(r0)
}

func (s *Store) AmbiguousLocalProxyReceipt(ctx context.Context, id uint) error {
	r0 := s.repository.AmbiguousLocalProxyReceipt(ctx, id)
	return backendError(r0)
}

func (s *Store) AmbiguousUDPGuardReceipt(ctx context.Context, id uint) error {
	r0 := s.repository.AmbiguousUDPGuardReceipt(ctx, id)
	return backendError(r0)
}

func (s *Store) AppendBatch(ctx context.Context, values []events.ProbeEvent) error {
	r0 := s.repository.AppendBatch(ctx, values)
	return backendError(r0)
}

func (s *Store) ArtifactByOperation(ctx context.Context, operationID string) (durable.ArtifactModel, error) {
	r0, r1 := s.repository.ArtifactByOperation(ctx, operationID)
	return durable.ArtifactModel(r0), backendError(r1)
}

func (s *Store) BeginLocalProxyReceipt(ctx context.Context, action, key, digest string) (durable.LocalProxyIdempotencyV1Model, bool, error) {
	r0, r1, r2 := s.repository.BeginLocalProxyReceipt(ctx, action, key, digest)
	return durable.LocalProxyIdempotencyV1Model(r0), r1, backendError(r2)
}

func (s *Store) BeginUDPGuardReceipt(ctx context.Context, action, key, digest string) (durable.UDPGuardIdempotencyV1Model, bool, error) {
	r0, r1, r2 := s.repository.BeginUDPGuardReceipt(ctx, action, key, digest)
	return durable.UDPGuardIdempotencyV1Model(r0), r1, backendError(r2)
}

func (s *Store) BindLocalProxyReceiptOperation(ctx context.Context, id uint, operationID string, operationRevision int) error {
	r0 := s.repository.BindLocalProxyReceiptOperation(ctx, id, operationID, operationRevision)
	return backendError(r0)
}

func (s *Store) BindUDPGuardReceiptOperation(ctx context.Context, id uint, operationID string, operationRevision int) error {
	r0 := s.repository.BindUDPGuardReceiptOperation(ctx, id, operationID, operationRevision)
	return backendError(r0)
}

func (s *Store) ClaimFrontingReceiptV2(ctx context.Context, value durable.FrontingIdempotencyV2Model) (durable.FrontingIdempotencyV2Model, bool, error) {
	r0, r1, r2 := s.repository.ClaimFrontingReceiptV2(ctx, repository.FrontingIdempotencyV2Model(value))
	return durable.FrontingIdempotencyV2Model(r0), r1, backendError(r2)
}

func (s *Store) CommitFirewallAuthority(ctx context.Context, operationID, expectedCompositionRevision, expectedContributionRevision string, replacement *durable.FirewallContributionModel, composition durable.FirewallCompositionModel, transitionState string) error {
	r0 := s.repository.CommitFirewallAuthority(ctx, operationID, expectedCompositionRevision, expectedContributionRevision, (*repository.FirewallContributionModel)(replacement), toFirewallCompositionModel(composition), transitionState)
	return backendError(r0)
}

func (s *Store) CompleteFrontingReceiptV2(ctx context.Context, action, key, digest string, operationID string, operationRevision int, response []byte, now int64) error {
	r0 := s.repository.CompleteFrontingReceiptV2(ctx, action, key, digest, operationID, operationRevision, response, now)
	return backendError(r0)
}

func (s *Store) CompleteLocalProxyReceipt(ctx context.Context, id uint, operationID string, operationRevision int, response any) error {
	r0 := s.repository.CompleteLocalProxyReceipt(ctx, id, operationID, operationRevision, response)
	return backendError(r0)
}

func (s *Store) CompleteUDPGuardReceipt(ctx context.Context, id uint, operationID string, operationRevision int, response any) error {
	r0 := s.repository.CompleteUDPGuardReceipt(ctx, id, operationID, operationRevision, response)
	return backendError(r0)
}

func (s *Store) CreateFirewallTransition(ctx context.Context, value durable.FirewallContributionTransitionModel) error {
	r0 := s.repository.CreateFirewallTransition(ctx, repository.FirewallContributionTransitionModel(value))
	return backendError(r0)
}

func (s *Store) CreateNativeFallbackOperation(ctx context.Context, model durable.NativeFallbackOperationModel) (durable.NativeFallbackOperationModel, error) {
	r0, r1 := s.repository.CreateNativeFallbackOperation(ctx, repository.NativeFallbackOperationModel(model))
	return durable.NativeFallbackOperationModel(r0), backendError(r1)
}

func (s *Store) DeleteArtifact(ctx context.Context, id uint) error {
	r0 := s.repository.DeleteArtifact(ctx, id)
	return backendError(r0)
}

func (s *Store) FirewallAuthority(ctx context.Context) (durable.FirewallAuthoritySnapshot, error) {
	r0, r1 := s.repository.FirewallAuthority(ctx)
	return fromFirewallAuthoritySnapshot(r0), backendError(r1)
}

func (s *Store) FirewallTransition(ctx context.Context, operationID string) (durable.FirewallContributionTransitionModel, error) {
	r0, r1 := s.repository.FirewallTransition(ctx, operationID)
	return durable.FirewallContributionTransitionModel(r0), backendError(r1)
}

func (s *Store) FirewallTransitions(ctx context.Context) ([]durable.FirewallContributionTransitionModel, error) {
	r0, r1 := s.repository.FirewallTransitions(ctx)
	return projectSlice(r0, fromFirewallContributionTransitionModel), backendError(r1)
}

func (s *Store) ForceUnlockOperation(ctx context.Context, operationID string, expectedRevision int, now int64) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.ForceUnlockOperation(ctx, operationID, expectedRevision, now)
	return durable.OperationLockModel(r0), backendError(r1)
}

func (s *Store) ForgetOperationState(ctx context.Context, operationID string, expectedRevision int, now int64) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.ForgetOperationState(ctx, operationID, expectedRevision, now)
	return durable.OperationLockModel(r0), backendError(r1)
}

func (s *Store) FrontingReceiptV2(ctx context.Context, action, key string) (durable.FrontingIdempotencyV2Model, error) {
	r0, r1 := s.repository.FrontingReceiptV2(ctx, action, key)
	return durable.FrontingIdempotencyV2Model(r0), backendError(r1)
}

func (s *Store) FrontingStatesV2(ctx context.Context) ([]durable.FrontingStateV2Model, error) {
	r0, r1 := s.repository.FrontingStatesV2(ctx)
	return projectSlice(r0, fromFrontingStateV2Model), backendError(r1)
}

func (s *Store) HeartbeatOperationLock(ctx context.Context, operationID, instanceID string, pid, revision int, now, expiresAt int64) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.HeartbeatOperationLock(ctx, operationID, instanceID, pid, revision, now, expiresAt)
	return durable.OperationLockModel(r0), backendError(r1)
}

func (s *Store) ListArtifacts(ctx context.Context) ([]durable.ArtifactModel, error) {
	r0, r1 := s.repository.ListArtifacts(ctx)
	return projectSlice(r0, fromArtifactModel), backendError(r1)
}

func (s *Store) ListIPAllowlist(ctx context.Context, page durable.PageQuery) ([]durable.IPAllowlistModel, int64, error) {
	r0, r1, r2 := s.repository.ListIPAllowlist(ctx, repository.PageQuery(page))
	return projectSlice(r0, fromIPAllowlistModel), r1, backendError(r2)
}

func (s *Store) ListNativeFallbackOperations(ctx context.Context, states []string) ([]durable.NativeFallbackOperationModel, error) {
	r0, r1 := s.repository.ListNativeFallbackOperations(ctx, states)
	return projectSlice(r0, fromNativeFallbackOperationModel), backendError(r1)
}

func (s *Store) ListOperationLocks(ctx context.Context, states []string) ([]durable.OperationLockModel, error) {
	r0, r1 := s.repository.ListOperationLocks(ctx, states)
	return projectSlice(r0, fromOperationLockModel), backendError(r1)
}

func (s *Store) LoadScore(ctx context.Context, key scoring.ScoreKey) (scoring.ScoreState, error) {
	r0, r1 := s.repository.LoadScore(ctx, key)
	return r0, backendError(r1)
}

func (s *Store) LoadSettings(ctx context.Context) (domain.Settings, bool, error) {
	r0, r1, r2 := s.repository.LoadSettings(ctx)
	return r0, r1, backendError(r2)
}

func (s *Store) LoadSettingsRevision(ctx context.Context) (domain.Settings, int, bool, error) {
	r0, r1, r2, r3 := s.repository.LoadSettingsRevision(ctx)
	return r0, r1, r2, backendError(r3)
}

func (s *Store) LocalProxyStateByOperation(ctx context.Context, operationID string) (durable.LocalProxyStateV1Model, error) {
	r0, r1 := s.repository.LocalProxyStateByOperation(ctx, operationID)
	return durable.LocalProxyStateV1Model(r0), backendError(r1)
}

func (s *Store) LocalProxyStates(ctx context.Context) ([]durable.LocalProxyStateV1Model, error) {
	r0, r1 := s.repository.LocalProxyStates(ctx)
	return projectSlice(r0, fromLocalProxyStateV1Model), backendError(r1)
}

func (s *Store) MarkFirewallTransitionMutation(ctx context.Context, operationID string, markerUnixNano int64) error {
	r0 := s.repository.MarkFirewallTransitionMutation(ctx, operationID, markerUnixNano)
	return backendError(r0)
}

func (s *Store) MarkFirewallTransitionMutationCompleted(ctx context.Context, operationID string, completedUnixNano int64) error {
	r0 := s.repository.MarkFirewallTransitionMutationCompleted(ctx, operationID, completedUnixNano)
	return backendError(r0)
}

func (s *Store) MarkOperationRecovery(ctx context.Context, operationID string, attempts int, at int64, code string) error {
	r0 := s.repository.MarkOperationRecovery(ctx, operationID, attempts, at, code)
	return backendError(r0)
}

func (s *Store) NativeFallbackOperation(ctx context.Context, operationID string) (durable.NativeFallbackOperationModel, error) {
	r0, r1 := s.repository.NativeFallbackOperation(ctx, operationID)
	return durable.NativeFallbackOperationModel(r0), backendError(r1)
}

func (s *Store) NativeFallbackState(ctx context.Context, resourceID string) (domain.NativeFallbackStateV1, error) {
	r0, r1 := s.repository.NativeFallbackState(ctx, resourceID)
	return r0, backendError(r1)
}

func (s *Store) OperationByHelperRevisionPrefix(ctx context.Context, prefix string) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.OperationByHelperRevisionPrefix(ctx, prefix)
	return durable.OperationLockModel(r0), backendError(r1)
}

func (s *Store) OperationByID(ctx context.Context, operationID string) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.OperationByID(ctx, operationID)
	return durable.OperationLockModel(r0), backendError(r1)
}

func (s *Store) OperationByIdempotencyKey(ctx context.Context, key string) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.OperationByIdempotencyKey(ctx, key)
	return durable.OperationLockModel(r0), backendError(r1)
}

func (s *Store) PersistObservationBatch(ctx context.Context, states []scoring.ScoreState, values []events.ProbeEvent, clear []scoring.ScoreKey) error {
	r0 := s.repository.PersistObservationBatch(ctx, states, values, clear)
	return backendError(r0)
}

func (s *Store) ProjectFrontingStateV2(ctx context.Context, value durable.FrontingStateV2Model) error {
	r0 := s.repository.ProjectFrontingStateV2(ctx, repository.FrontingStateV2Model(value))
	return backendError(r0)
}

func (s *Store) ProtectedArtifactOperations(ctx context.Context) (map[string]string, error) {
	r0, r1 := s.repository.ProtectedArtifactOperations(ctx)
	return r0, backendError(r1)
}

func (s *Store) PruneFirewallHistory(ctx context.Context, keepCount int, cutoff int64, keepBytes int64) (durable.FirewallHistoryPruneResult, error) {
	r0, r1 := s.repository.PruneFirewallHistory(ctx, keepCount, cutoff, keepBytes)
	return durable.FirewallHistoryPruneResult(r0), backendError(r1)
}

func (s *Store) PruneIdempotencyReceipts(ctx context.Context, keepCount int, cutoff int64, keepBytes int64) (durable.IdempotencyReceiptPruneResult, error) {
	r0, r1 := s.repository.PruneIdempotencyReceipts(ctx, keepCount, cutoff, keepBytes)
	return durable.IdempotencyReceiptPruneResult(r0), backendError(r1)
}

func (s *Store) PruneNativeFallbackHistory(ctx context.Context, keepCount int, cutoff int64, keepBytes int64) (durable.NativeFallbackHistoryPruneResult, error) {
	r0, r1 := s.repository.PruneNativeFallbackHistory(ctx, keepCount, cutoff, keepBytes)
	return durable.NativeFallbackHistoryPruneResult(r0), backendError(r1)
}

func (s *Store) Purge(ctx context.Context, policy events.RetentionPolicy) (events.PurgeResult, error) {
	r0, r1 := s.repository.Purge(ctx, policy)
	return r0, backendError(r1)
}

func (s *Store) ReclaimOperationLock(ctx context.Context, update durable.ReclaimOperationLockUpdate) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.ReclaimOperationLock(ctx, repository.ReclaimOperationLockUpdate(update))
	return durable.OperationLockModel(r0), backendError(r1)
}

func (s *Store) RecordFirewallObservation(ctx context.Context, value durable.FirewallObservationModel, expectedCompositionRevision string) error {
	r0 := s.repository.RecordFirewallObservation(ctx, repository.FirewallObservationModel(value), expectedCompositionRevision)
	return backendError(r0)
}

func (s *Store) RecordFirewallTransitionHealth(ctx context.Context, operationID, providerInstance string, generation uint64, observationRevision string, startedUnixNano, completedUnixNano, expiresUnixNano int64) error {
	r0 := s.repository.RecordFirewallTransitionHealth(ctx, operationID, providerInstance, generation, observationRevision, startedUnixNano, completedUnixNano, expiresUnixNano)
	return backendError(r0)
}

func (s *Store) RecordRuntimeRecoveryFailure(ctx context.Context, operation durable.OperationLockModel, attempts int, now int64) error {
	r0 := s.repository.RecordRuntimeRecoveryFailure(ctx, repository.OperationLockModel(operation), attempts, now)
	return backendError(r0)
}

func (s *Store) RecoverOperationLock(ctx context.Context, update durable.RecoveryOperationLockUpdate) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.RecoverOperationLock(ctx, repository.RecoveryOperationLockUpdate(update))
	return durable.OperationLockModel(r0), backendError(r1)
}

func (s *Store) ReplayLocalProxyReceipt(ctx context.Context, action, key, digest string) (durable.LocalProxyIdempotencyV1Model, bool, error) {
	r0, r1, r2 := s.repository.ReplayLocalProxyReceipt(ctx, action, key, digest)
	return durable.LocalProxyIdempotencyV1Model(r0), r1, backendError(r2)
}

func (s *Store) ReservationMirror(ctx context.Context, operationID string) (durable.FallbackTargetLeaseModel, error) {
	r0, r1 := s.repository.ReservationMirror(ctx, operationID)
	return durable.FallbackTargetLeaseModel(r0), backendError(r1)
}

func (s *Store) RetireFirewallAuthorityAfterRuntimeLoss(ctx context.Context, operationID string, operationRevision int, expectedCompositionRevision string) error {
	r0 := s.repository.RetireFirewallAuthorityAfterRuntimeLoss(ctx, operationID, operationRevision, expectedCompositionRevision)
	return backendError(r0)
}

func (s *Store) SaveArtifact(ctx context.Context, item *durable.ArtifactModel) error {
	r0 := s.repository.SaveArtifact(ctx, (*repository.ArtifactModel)(item))
	return backendError(r0)
}

func (s *Store) SaveLocalProxyState(ctx context.Context, value durable.LocalProxyStateV1Model) error {
	r0 := s.repository.SaveLocalProxyState(ctx, repository.LocalProxyStateV1Model(value))
	return backendError(r0)
}

func (s *Store) SaveScore(ctx context.Context, state scoring.ScoreState) error {
	r0 := s.repository.SaveScore(ctx, state)
	return backendError(r0)
}

func (s *Store) SaveUDPGuardState(ctx context.Context, value durable.UDPGuardStateV1Model) error {
	r0 := s.repository.SaveUDPGuardState(ctx, repository.UDPGuardStateV1Model(value))
	return backendError(r0)
}

func (s *Store) SetFirewallTransitionState(ctx context.Context, operationID, from, to string) error {
	r0 := s.repository.SetFirewallTransitionState(ctx, operationID, from, to)
	return backendError(r0)
}

func (s *Store) UDPGuardStates(ctx context.Context) ([]durable.UDPGuardStateV1Model, error) {
	r0, r1 := s.repository.UDPGuardStates(ctx)
	return projectSlice(r0, fromUDPGuardStateV1Model), backendError(r1)
}

func (s *Store) UpdateFirewallRuntime(ctx context.Context, operationID string, operationRevision int, compositionRevision string, before, after durable.FirewallRuntimeBinding) error {
	r0 := s.repository.UpdateFirewallRuntime(ctx, operationID, operationRevision, compositionRevision, repository.FirewallRuntimeBinding(before), repository.FirewallRuntimeBinding(after))
	return backendError(r0)
}

func (s *Store) UpdateOperationLockFenced(ctx context.Context, update durable.FencedOperationLockUpdate) (durable.OperationLockModel, error) {
	r0, r1 := s.repository.UpdateOperationLockFenced(ctx, repository.FencedOperationLockUpdate(update))
	return durable.OperationLockModel(r0), backendError(r1)
}
