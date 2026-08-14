package sshmanagement

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

type AuditEventV1 struct {
	Event       string
	OperationID string
	State       domain.CandidateState
	ReasonCode  domain.ReasonCode
	Revision    uint64
}

type PreviewRequestV1 struct {
	Policy       domain.DesiredPolicyV1 `json:"policy"`
	Acknowledged bool                   `json:"acknowledged"`
}

type PreviewV1 struct {
	Policy           domain.DesiredPolicyV1               `json:"policy"`
	Posture          *domain.SSHPostureV1                 `json:"posture,omitempty"`
	Capabilities     domain.CapabilitySetV1               `json:"capabilities"`
	Endpoints        []hostresources.ManagementEndpointV1 `json:"endpoints"`
	RecoveryPaths    []hostresources.RecoveryPathV1       `json:"recoveryPaths"`
	Preservation     domain.ManagementPreservationPlanV1  `json:"preservation"`
	CandidateDigest  string                               `json:"candidateDigest,omitempty"`
	ProviderRevision string                               `json:"providerRevision,omitempty"`
	PostureRevision  string                               `json:"postureRevision,omitempty"`
	EndpointRevision string                               `json:"endpointRevision"`
	RecoveryRevision string                               `json:"recoveryRevision"`
	Possible         bool                                 `json:"possible"`
	ReasonCodes      []domain.ReasonCode                  `json:"reasonCodes,omitempty"`
	Revision         string                               `json:"revision"`
}

type StartRequestV1 struct {
	Policy                   domain.DesiredPolicyV1 `json:"policy"`
	IdempotencyKey           string                 `json:"idempotencyKey"`
	ExpectedPreviewRevision  string                 `json:"expectedPreviewRevision"`
	ExpectedPostureRevision  string                 `json:"expectedPostureRevision"`
	ExpectedEndpointRevision string                 `json:"expectedEndpointRevision"`
	ExpectedRecoveryRevision string                 `json:"expectedRecoveryRevision"`
	ExpectedProviderRevision string                 `json:"expectedProviderRevision"`
	EndpointID               string                 `json:"endpointId"`
	PrincipalID              string                 `json:"principalId"`
	AuthenticationClass      string                 `json:"authenticationClass"`
	Acknowledged             bool                   `json:"acknowledged"`
}

type StartResultV1 struct {
	Candidate domain.CandidateV1 `json:"candidate"`
	Verifier  string             `json:"-"`
}

type ConfirmRequestV1 struct {
	OperationID         string `json:"operationId"`
	ExpectedRevision    uint64 `json:"expectedRevision"`
	ProviderEvidenceRef string `json:"providerEvidenceRef"`
}

type RollbackRequestV1 struct {
	OperationID      string `json:"operationId"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

type Manager struct {
	Repository        Repository
	Provider          Provider
	Now               func() time.Time
	Random            io.Reader
	Endpoints         func(context.Context, time.Time) []hostresources.ManagementEndpointV1
	Evidence          func(context.Context, time.Time) managementregistry.EvidenceSnapshot
	WatchdogAvailable bool
	Audit             func(context.Context, AuditEventV1)
	mu                sync.Mutex
}

func NewManager(repository Repository, provider Provider) *Manager {
	if provider == nil {
		provider = UnavailableProvider{}
	}
	return &Manager{Repository: repository, Provider: provider, Now: time.Now, Random: rand.Reader,
		Endpoints: managementregistry.CurrentEndpoints, Evidence: managementregistry.RecoveryEvidence,
		WatchdogAvailable: true}
}

func DefaultManagerWithProvider(provider Provider) *Manager {
	return NewManager(Repository{DB: dbsqlite.DB}, provider)
}

func (m *Manager) now() time.Time {
	if m != nil && m.Now != nil {
		return m.Now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

func (m *Manager) capabilities(ctx context.Context) domain.CapabilitySetV1 {
	if m == nil || m.Provider == nil {
		return UnavailableProvider{}.Capabilities(ctx)
	}
	return m.Provider.Capabilities(ctx)
}

func (m *Manager) Capabilities(ctx context.Context) domain.CapabilitySetV1 {
	return m.capabilities(ctx)
}

func (m *Manager) EndpointSnapshot(ctx context.Context) []hostresources.ManagementEndpointV1 {
	return m.endpoints(ctx, m.now())
}

func (m *Manager) RecoverySnapshot(ctx context.Context) managementregistry.EvidenceSnapshot {
	now := m.now()
	value := m.evidence(ctx, now)
	value.Paths = effectiveEvidence(value.Paths, m.endpoints(ctx, now), now)
	return value
}

func (m *Manager) LatestPosture(ctx context.Context) (*domain.SSHPostureV1, error) {
	posture, err := m.Repository.LatestPosture(ctx)
	if err != nil {
		return nil, err
	}
	return &posture, nil
}

func (m *Manager) Candidate(ctx context.Context, operationID string) (domain.CandidateV1, error) {
	return m.Repository.Candidate(ctx, operationID)
}

func (m *Manager) Timeline(ctx context.Context, operationID string) ([]model.SSHManagementJournal, error) {
	return m.Repository.Journal(ctx, operationID)
}

func (m *Manager) ReconnectState(ctx context.Context, operationID string) (map[string]any, error) {
	candidate, err := m.Repository.Candidate(ctx, operationID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"operationId": candidate.OperationID, "state": candidate.State, "revision": candidate.Revision,
		"required": candidate.State == domain.StateReconnectRequired, "expiresAt": candidate.ReconnectExpiresAt}
	if challenge, challengeErr := m.Repository.Challenge(ctx, operationID); challengeErr == nil {
		result["consumed"] = challenge.ConsumedAt != 0
	} else if candidate.State == domain.StateReconnectRequired || !errors.Is(challengeErr, gorm.ErrRecordNotFound) {
		return nil, challengeErr
	}
	return result, nil
}

func (m *Manager) Preview(ctx context.Context, request PreviewRequestV1) (PreviewV1, error) {
	if err := request.Policy.Validate(); err != nil {
		return PreviewV1{}, err
	}
	now := m.now()
	preview := PreviewV1{Policy: request.Policy, Capabilities: m.capabilities(ctx), Endpoints: m.endpoints(ctx, now)}
	evidence := m.evidence(ctx, now)
	preview.RecoveryPaths = effectiveEvidence(evidence.Paths, preview.Endpoints, now)
	observation, observeErr := m.Provider.Observe(ctx)
	if observeErr != nil {
		preview.ReasonCodes = append(preview.ReasonCodes, domain.ErrorCode(observeErr))
		preview.ReasonCodes = append(preview.ReasonCodes, preview.Capabilities.ReasonCodes...)
		if preview.Capabilities.Stage != domain.AvailabilityAvailable || preview.Capabilities.Validate != domain.AvailabilityAvailable ||
			preview.Capabilities.Reload != domain.AvailabilityAvailable || preview.Capabilities.Rollback != domain.AvailabilityAvailable {
			preview.ReasonCodes = append(preview.ReasonCodes, domain.ReasonProductionMutationAbsent)
		}
		preview.Preservation = domain.BuildPreservationPlan(domain.PreservationInput{Before: preview.Endpoints, After: preview.Endpoints, Recovery: preview.RecoveryPaths, Now: now, Policy: request.Policy, Watchdog: m.WatchdogAvailable})
		preview.EndpointRevision = domain.Revision(preview.Endpoints)
		preview.RecoveryRevision = domain.Revision(preview.RecoveryPaths)
		preview.ReasonCodes = append(preview.ReasonCodes, preview.Preservation.ReasonCodes...)
		if !request.Acknowledged {
			preview.ReasonCodes = append(preview.ReasonCodes, domain.ReasonAcknowledgementMissing)
		}
		preview.ReasonCodes = normalizedReasons(preview.ReasonCodes)
		preview.Revision = previewRevision(preview)
		return preview, nil
	}
	if err := observation.Posture.Validate(now); err != nil {
		preview.ReasonCodes = append(preview.ReasonCodes, domain.ErrorCode(err))
	} else {
		preview.Posture = &observation.Posture
		preview.PostureRevision = observation.Posture.SemanticRevision
		preview.ProviderRevision = observation.ProviderRevision
		preview.Endpoints = mergeEndpoints(preview.Endpoints, observation.Posture.Endpoints)
		preview.RecoveryPaths = effectiveEvidence(evidence.Paths, preview.Endpoints, now)
	}
	preview.Preservation = domain.BuildPreservationPlan(domain.PreservationInput{Before: preview.Endpoints, After: preview.Endpoints, Recovery: preview.RecoveryPaths, Now: now, Policy: request.Policy, Watchdog: m.WatchdogAvailable})
	preview.EndpointRevision = domain.Revision(preview.Endpoints)
	preview.RecoveryRevision = domain.Revision(preview.RecoveryPaths)
	preview.ReasonCodes = append(preview.ReasonCodes, preview.Preservation.ReasonCodes...)
	if !request.Acknowledged {
		preview.ReasonCodes = append(preview.ReasonCodes, domain.ReasonAcknowledgementMissing)
	}
	if preview.Capabilities.Stage != domain.AvailabilityAvailable || preview.Capabilities.Validate != domain.AvailabilityAvailable || preview.Capabilities.Reload != domain.AvailabilityAvailable || preview.Capabilities.Rollback != domain.AvailabilityAvailable {
		preview.ReasonCodes = append(preview.ReasonCodes, domain.ReasonProductionMutationAbsent)
	}
	content, renderErr := request.Policy.RenderManagedDropIn()
	if renderErr == nil {
		preview.CandidateDigest = domain.Revision(content)
	}
	preview.ReasonCodes = normalizedReasons(preview.ReasonCodes)
	preview.Possible = preview.Posture != nil && len(preview.ReasonCodes) == 0 && preview.Preservation.Safe
	preview.Revision = previewRevision(preview)
	return preview, nil
}

func (m *Manager) Start(ctx context.Context, request StartRequestV1) (StartResultV1, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !safeIdentifier(request.IdempotencyKey, 96) || !safeIdentifier(request.EndpointID, 256) || !safeIdentifier(request.PrincipalID, 256) || !oneOf(request.AuthenticationClass, "publickey", "certificate") {
		return StartResultV1{}, domain.NewError("candidate", domain.ReasonMalformedProviderEvidence)
	}
	if replay, err := m.Repository.CandidateByIdempotency(ctx, request.IdempotencyKey); err == nil {
		if !samePolicy(replay.Policy, request.Policy) {
			return StartResultV1{}, domain.NewError("candidate", domain.ReasonIdempotencyConflict)
		}
		return StartResultV1{Candidate: replay}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return StartResultV1{}, err
	}
	preview, err := m.Preview(ctx, PreviewRequestV1{Policy: request.Policy, Acknowledged: request.Acknowledged})
	if err != nil {
		return StartResultV1{}, err
	}
	if request.ExpectedPreviewRevision != preview.Revision || preview.Posture == nil || request.ExpectedPostureRevision != preview.Posture.SemanticRevision ||
		request.ExpectedEndpointRevision != preview.EndpointRevision || request.ExpectedRecoveryRevision != preview.RecoveryRevision ||
		request.ExpectedProviderRevision != preview.ProviderRevision {
		return StartResultV1{}, domain.NewError("candidate", domain.ReasonRevisionMismatch)
	}
	if !preview.Possible {
		code := domain.ReasonProviderUnavailable
		if len(preview.ReasonCodes) > 0 {
			code = preview.ReasonCodes[0]
		}
		return StartResultV1{}, domain.NewError("candidate", code)
	}
	now := m.now()
	operationID, err := m.randomID("ssh-operation:", 16)
	if err != nil {
		return StartResultV1{}, err
	}
	candidate := domain.CandidateV1{Schema: domain.CandidateSchemaV1, OperationID: operationID, IdempotencyKey: request.IdempotencyKey,
		State: domain.StateDraft, Revision: 1, Policy: request.Policy, Preservation: preview.Preservation,
		CandidateDigest: preview.CandidateDigest, PostureRevision: preview.Posture.SemanticRevision,
		EndpointRevision: domain.Revision(preview.Endpoints), RecoveryRevision: domain.Revision(preview.RecoveryPaths), ProviderRevision: preview.ProviderRevision,
		BinaryRevision: preview.Posture.BinaryRevision, ServiceRevision: preview.Posture.ServiceRevision,
		ConfigurationRevision: preview.Posture.ConfigurationRevision, EarliestSafetyExpiry: preview.Preservation.EarliestSafetyExpiry,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	candidate.BindingDigest = domain.BindingDigest(candidate)
	if err := m.Repository.SavePosture(ctx, *preview.Posture, now); err != nil {
		return StartResultV1{}, err
	}
	if err := m.Repository.CreateCandidateWithJournal(ctx, candidate, "draft_created", "", now); err != nil {
		return StartResultV1{}, repositoryConflict(err)
	}
	m.emitAudit(ctx, candidate, "draft_created", "")
	candidate, err = m.advance(ctx, candidate, domain.StatePreflighted, "preflight_completed", "")
	if err != nil {
		return StartResultV1{}, err
	}
	content, err := candidate.Policy.RenderManagedDropIn()
	if err != nil {
		return StartResultV1{}, err
	}
	staged, err := m.Provider.StageManagedDropIn(ctx, StageRequestV1{Fence: m.providerFence(candidate), ManagedContent: content})
	if err != nil {
		if staged.ArtifactDigest != "" && staged.Prior.Digest != "" {
			return StartResultV1{}, errors.Join(err, m.rollbackAfterStage(ctx, candidate, staged, domain.ReasonConfigurationMismatch))
		}
		_, transitionErr := m.advance(ctx, candidate, domain.StateManualRecoveryRequired, "stage_outcome_requires_recovery", domain.ReasonConfigurationMismatch)
		return StartResultV1{}, errors.Join(err, transitionErr)
	}
	if staged.ArtifactDigest != candidate.CandidateDigest || staged.ProviderRevision != candidate.ProviderRevision || staged.Prior.Digest == "" {
		return StartResultV1{}, m.rollbackAfterStage(ctx, candidate, staged, domain.ReasonRevisionMismatch)
	}
	if err := m.Repository.SaveCheckpoint(ctx, candidate.OperationID, staged.Prior, staged.ArtifactDigest, staged.ConfigurationRevision, now); err != nil {
		return StartResultV1{}, m.rollbackAfterStage(ctx, candidate, staged, domain.ReasonOperationStateConflict)
	}
	candidate.BeforeArtifactDigest, candidate.AfterArtifactDigest = staged.Prior.Digest, staged.ArtifactDigest
	candidate.ConfigurationRevision = staged.ConfigurationRevision
	candidate.BindingDigest = domain.BindingDigest(candidate)
	candidate, err = m.advance(ctx, candidate, domain.StateStaged, "dropin_staged", "")
	if err != nil {
		return StartResultV1{}, m.rollbackAfterStage(ctx, candidate, staged, domain.ReasonOperationStateConflict)
	}
	validated, err := m.Provider.ValidateManagedDropIn(ctx, ValidationRequestV1{Fence: m.providerFence(candidate), ArtifactDigest: candidate.AfterArtifactDigest})
	if err != nil || !validated.SyntaxValid || !validated.EffectiveValid || len(validated.ReasonCodes) != 0 || validated.ProviderRevision != candidate.ProviderRevision {
		return StartResultV1{}, m.failAndRollback(ctx, candidate, domain.ReasonConfigurationMismatch)
	}
	candidate, err = m.advance(ctx, candidate, domain.StateValidated, "candidate_validated", "")
	if err != nil {
		return StartResultV1{}, m.failAndRollback(ctx, candidate, domain.ReasonOperationStateConflict)
	}
	candidate, err = m.advance(ctx, candidate, domain.StateReloadPending, "reload_pending", "")
	if err != nil {
		return StartResultV1{}, m.failAndRollback(ctx, candidate, domain.ReasonOperationStateConflict)
	}
	reloaded, err := m.Provider.ReloadSelectedService(ctx, ReloadRequestV1{Fence: m.providerFence(candidate), ArtifactDigest: candidate.AfterArtifactDigest})
	if err != nil || reloaded.ProviderRevision != candidate.ProviderRevision || reloaded.ServiceRevision == "" || reloaded.ConfigurationRevision == "" {
		return StartResultV1{}, m.failAndRollback(ctx, candidate, domain.ReasonConfigurationMismatch)
	}
	candidate.ServiceRevision, candidate.ConfigurationRevision = reloaded.ServiceRevision, reloaded.ConfigurationRevision
	candidate.ReconnectExpiresAt = minInt64(now.Add(domain.MaxChallengeLifetime).Unix(), candidate.EarliestSafetyExpiry)
	candidate.BindingDigest = domain.BindingDigest(candidate)
	verifier, err := m.randomID("ssh-proof:", 24)
	if err != nil {
		return StartResultV1{}, m.failAndRollback(ctx, candidate, domain.ReasonReconnectProofInvalid)
	}
	challenge := domain.ReconnectChallengeV1{Schema: domain.ChallengeSchemaV1, OperationID: candidate.OperationID, CandidateDigest: candidate.CandidateDigest,
		MarkerDigest: domain.Revision(struct{ Operation, Candidate string }{candidate.OperationID, candidate.CandidateDigest}), EndpointID: request.EndpointID,
		PrincipalID: request.PrincipalID, AuthenticationClass: request.AuthenticationClass, ServiceRevision: candidate.ServiceRevision,
		BinaryRevision: candidate.BinaryRevision, ConfigurationRevision: candidate.ConfigurationRevision, VerifierDigest: digestString(verifier),
		IssuedAt: now.Unix(), ExpiresAt: candidate.ReconnectExpiresAt, Revision: 1}
	if err := m.Repository.SaveChallenge(ctx, challenge); err != nil {
		return StartResultV1{}, m.failAndRollback(ctx, candidate, domain.ReasonOperationStateConflict)
	}
	if armer, ok := m.Provider.(ReconnectArmer); ok {
		if err := armer.ArmReconnect(ctx, ReconnectProofV1{Fence: m.providerFence(candidate), MarkerDigest: challenge.MarkerDigest,
			Verifier: verifier, EndpointID: challenge.EndpointID, PrincipalID: challenge.PrincipalID,
			AuthenticationClass: challenge.AuthenticationClass}, challenge.ExpiresAt); err != nil {
			return StartResultV1{}, m.failAndRollback(ctx, candidate, domain.ReasonReconnectProofInvalid)
		}
	}
	candidate, err = m.advance(ctx, candidate, domain.StateReconnectRequired, "reconnect_required", "")
	if err != nil {
		return StartResultV1{}, m.failAndRollback(ctx, candidate, domain.ReasonOperationStateConflict)
	}
	return StartResultV1{Candidate: candidate, Verifier: verifier}, nil
}

func (m *Manager) Confirm(ctx context.Context, request ConfirmRequestV1) (domain.CandidateV1, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate, err := m.Repository.Candidate(ctx, request.OperationID)
	if err != nil {
		return domain.CandidateV1{}, err
	}
	if candidate.State != domain.StateReconnectRequired || candidate.Revision != request.ExpectedRevision || candidate.RestoredUntrusted {
		return domain.CandidateV1{}, domain.NewError("confirm", domain.ReasonOperationStateConflict)
	}
	now := m.now()
	challenge, err := m.Repository.Challenge(ctx, candidate.OperationID)
	if err != nil || challenge.ExpiresAt <= now.Unix() || challenge.ConsumedAt != 0 || challenge.CandidateDigest != candidate.CandidateDigest ||
		challenge.BinaryRevision != candidate.BinaryRevision || challenge.ServiceRevision != candidate.ServiceRevision ||
		challenge.ConfigurationRevision != candidate.ConfigurationRevision || !constantDigest(request.ProviderEvidenceRef, challenge.VerifierDigest) {
		return domain.CandidateV1{}, domain.NewError("confirm", domain.ReasonReconnectProofInvalid)
	}
	proof, err := m.Provider.VerifyReconnect(ctx, ReconnectProofV1{Fence: m.providerFence(candidate),
		MarkerDigest: challenge.MarkerDigest, Verifier: request.ProviderEvidenceRef, EndpointID: challenge.EndpointID, PrincipalID: challenge.PrincipalID,
		AuthenticationClass: challenge.AuthenticationClass})
	if err != nil || !proof.Verified || !proof.Independent || !proof.FreshSession || !proof.OperationBound || proof.EndpointID != challenge.EndpointID ||
		proof.PrincipalID != challenge.PrincipalID || proof.AuthenticationClass != challenge.AuthenticationClass || proof.EvidenceRevision == "" {
		return domain.CandidateV1{}, domain.NewError("confirm", domain.ReasonReconnectProofInvalid)
	}
	path := hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:" + domain.Revision(struct{ Operation, Endpoint, Principal string }{candidate.OperationID, proof.EndpointID, proof.PrincipalID}),
		Kind: string(hostresources.ManagementSSH), EndpointID: proof.EndpointID, PrincipalID: proof.PrincipalID, VerificationMethod: "fresh_ssh_login",
		EvidenceProvider: m.Provider.ProviderID(), TargetOperation: candidate.OperationID, VerifiedAt: now.Unix(), ExpiresAt: minInt64(now.Add(domain.MaxRecoveryLifetime).Unix(), candidate.EarliestSafetyExpiry),
		IndependenceClass: "independent_reconnect", VerificationState: "verified", OperationBound: true, SingleUse: true, Revision: 1,
		SourceRevision: proof.EvidenceRevision, ConfigurationRevision: candidate.ConfigurationRevision, ServiceRevision: candidate.ServiceRevision,
		BinaryRevision: candidate.BinaryRevision, ProducerRevision: evidenceProducerRevision}
	inspected, inspectErr := m.Provider.InspectManagedDropIn(ctx, InspectRequestV1{Fence: m.providerFence(candidate)})
	observed, observeErr := m.Provider.Observe(ctx)
	currentEndpoints := m.endpoints(ctx, now)
	currentEvidence := m.evidence(ctx, now).Paths
	currentEvidence = append(currentEvidence, path)
	preservation := domain.BuildPreservationPlan(domain.PreservationInput{Before: candidate.Preservation.BeforeEndpoints, After: currentEndpoints,
		Recovery: effectiveEvidence(currentEvidence, currentEndpoints, now), Now: now, Policy: candidate.Policy, Watchdog: m.WatchdogAvailable})
	if inspectErr != nil || observeErr != nil || inspected.Symlink || !inspected.Present || inspected.ArtifactDigest != candidate.AfterArtifactDigest ||
		observed.Posture.Validate(now) != nil || observed.ProviderRevision != candidate.ProviderRevision ||
		observed.Posture.BinaryRevision != candidate.BinaryRevision || observed.Posture.ServiceRevision != candidate.ServiceRevision ||
		observed.Posture.ConfigurationRevision != candidate.ConfigurationRevision || !preservation.Safe {
		return domain.CandidateV1{}, m.failAndRollback(ctx, candidate, domain.ReasonReconnectProofInvalid)
	}
	previous := candidate
	candidate.Preservation = preservation
	candidate.ReconciledAt = now.Unix()
	candidate.BindingDigest = domain.BindingDigest(candidate)
	if !domain.TransitionAllowed(candidate.State, domain.StateCommitted) {
		return domain.CandidateV1{}, m.failAndRollback(ctx, previous, domain.ReasonOperationStateConflict)
	}
	candidate.State = domain.StateCommitted
	candidate.Revision++
	candidate.UpdatedAt = now.Unix()
	if err := m.Repository.CommitCandidateCASWithJournalAndEvidence(ctx, candidate, previous.Revision, previous.State, challenge.Revision, path, now); err != nil {
		return domain.CandidateV1{}, errors.Join(err, m.failAndRollback(ctx, previous, domain.ReasonOperationStateConflict))
	}
	m.emitAudit(ctx, candidate, "candidate_committed", "")
	return candidate, nil
}

func (m *Manager) Rollback(ctx context.Context, request RollbackRequestV1) (domain.CandidateV1, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate, err := m.Repository.Candidate(ctx, request.OperationID)
	if err != nil {
		return domain.CandidateV1{}, err
	}
	if candidate.Revision != request.ExpectedRevision || candidate.State.Terminal() || candidate.State == domain.StateDraft || candidate.State == domain.StatePreflighted {
		return domain.CandidateV1{}, domain.NewError("rollback", domain.ReasonOperationStateConflict)
	}
	err = m.rollback(ctx, candidate, domain.ReasonReconnectProofInvalid)
	if err != nil {
		if current, loadErr := m.Repository.Candidate(ctx, request.OperationID); loadErr == nil {
			return current, err
		}
		return domain.CandidateV1{}, err
	}
	return m.Repository.Candidate(ctx, request.OperationID)
}

func (m *Manager) ReconcileExpired(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate, err := m.Repository.ActiveCandidate(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := m.now().Unix()
	if candidate.RestoredUntrusted {
		candidate.ReasonCodes = append(candidate.ReasonCodes, domain.ReasonRestoredStateUntrusted)
		_, err = m.advance(ctx, candidate, domain.StateManualRecoveryRequired, "restored_state_requires_recovery", domain.ReasonRestoredStateUntrusted)
		return err
	}
	if (candidate.State == domain.StateReconnectRequired && candidate.ReconnectExpiresAt > 0 && candidate.ReconnectExpiresAt <= now) || candidate.EarliestSafetyExpiry <= now {
		return m.rollback(ctx, candidate, domain.ReasonReconnectProofInvalid)
	}
	return nil
}

// ReconcileStartup resolves every interrupted, non-terminal workflow before
// the periodic watchdog begins. A persisted checkpoint proves that staging may
// have happened even when the candidate state did not advance yet.
func (m *Manager) ReconcileStartup(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate, err := m.Repository.ActiveCandidate(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if candidate.RestoredUntrusted {
		candidate.ReasonCodes = append(candidate.ReasonCodes, domain.ReasonRestoredStateUntrusted)
		_, err = m.advance(ctx, candidate, domain.StateManualRecoveryRequired, "restored_state_requires_recovery", domain.ReasonRestoredStateUntrusted)
		return err
	}
	if candidate.State == domain.StateDraft {
		_, err = m.advance(ctx, candidate, domain.StateManualRecoveryRequired, "startup_state_requires_recovery", domain.ReasonOperationStateConflict)
		return err
	}
	if candidate.State == domain.StatePreflighted {
		checkpoint, checkpointErr := m.Repository.Checkpoint(ctx, candidate.OperationID)
		if checkpointErr != nil {
			_, transitionErr := m.advance(ctx, candidate, domain.StateManualRecoveryRequired, "startup_state_requires_recovery", domain.ReasonRollbackVerification)
			return errors.Join(checkpointErr, transitionErr)
		}
		candidate.BeforeArtifactDigest = checkpoint.PriorDigest
		candidate.AfterArtifactDigest = checkpoint.StagedArtifactDigest
		candidate.ConfigurationRevision = checkpoint.StagedConfigurationRevision
		candidate.BindingDigest = domain.BindingDigest(candidate)
		candidate, err = m.advance(ctx, candidate, domain.StateStaged, "startup_staged_checkpoint_recovered", domain.ReasonOperationStateConflict)
		if err != nil {
			return err
		}
	}
	return m.rollback(ctx, candidate, domain.ReasonOperationStateConflict)
}

func (m *Manager) StartWatchdog(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.ReconcileExpired(ctx); err != nil {
				logger.Warning("SSH management watchdog reconciliation failed")
			}
		}
	}
}

func (m *Manager) advance(ctx context.Context, candidate domain.CandidateV1, state domain.CandidateState, event string, reason domain.ReasonCode) (domain.CandidateV1, error) {
	if !domain.TransitionAllowed(candidate.State, state) {
		return domain.CandidateV1{}, domain.NewError("transition", domain.ReasonOperationStateConflict)
	}
	expectedRevision, expectedState := candidate.Revision, candidate.State
	candidate.State = state
	candidate.Revision++
	candidate.UpdatedAt = m.now().Unix()
	if reason != "" {
		candidate.ReasonCodes = normalizedReasons(append(candidate.ReasonCodes, reason))
	}
	if err := m.Repository.UpdateCandidateCASWithJournal(ctx, candidate, expectedRevision, expectedState, event, reason, m.now()); err != nil {
		return domain.CandidateV1{}, err
	}
	m.emitAudit(ctx, candidate, event, reason)
	return candidate, nil
}

func (m *Manager) rollbackAfterStage(ctx context.Context, candidate domain.CandidateV1, staged StageResultV1, reason domain.ReasonCode) error {
	candidate.BeforeArtifactDigest, candidate.AfterArtifactDigest = staged.Prior.Digest, staged.ArtifactDigest
	candidate.ConfigurationRevision = staged.ConfigurationRevision
	candidate.BindingDigest = domain.BindingDigest(candidate)
	if candidate.State == domain.StatePreflighted {
		advanced, err := m.advance(ctx, candidate, domain.StateStaged, "stage_recovery_required", reason)
		if err == nil {
			candidate = advanced
		}
	}
	return errors.Join(domain.NewError("candidate", reason), m.rollbackWithPrior(ctx, candidate, staged.Prior, reason))
}

func (m *Manager) failAndRollback(ctx context.Context, candidate domain.CandidateV1, reason domain.ReasonCode) error {
	return errors.Join(domain.NewError("candidate", reason), m.rollback(ctx, candidate, reason))
}

func (m *Manager) rollback(ctx context.Context, candidate domain.CandidateV1, reason domain.ReasonCode) error {
	checkpoint, err := m.Repository.Checkpoint(ctx, candidate.OperationID)
	if err != nil {
		_, transitionErr := m.advance(ctx, candidate, domain.StateManualRecoveryRequired, "rollback_checkpoint_missing", domain.ReasonRollbackVerification)
		return errors.Join(err, transitionErr)
	}
	prior := PriorArtifactV1{Present: checkpoint.PriorPresent, Content: append([]byte(nil), checkpoint.PriorContent...), Owner: checkpoint.PriorOwner,
		Group: checkpoint.PriorGroup, ModeClass: checkpoint.PriorModeClass, Mode: checkpoint.PriorMode, Digest: checkpoint.PriorDigest}
	return m.rollbackWithPrior(ctx, candidate, prior, reason)
}

func (m *Manager) rollbackWithPrior(ctx context.Context, candidate domain.CandidateV1, prior PriorArtifactV1, reason domain.ReasonCode) error {
	if candidate.RollbackAttempts != 0 {
		if candidate.State != domain.StateRollbackPending {
			return domain.NewError("rollback", domain.ReasonOperationStateConflict)
		}
		_, err := m.advance(ctx, candidate, domain.StateManualRecoveryRequired, "rollback_not_repeatable", domain.ReasonRollbackVerification)
		return err
	}
	if candidate.State != domain.StateRollbackPending {
		var err error
		candidate, err = m.advance(ctx, candidate, domain.StateRollbackPending, "rollback_pending", reason)
		if err != nil {
			return err
		}
	}
	candidate.RollbackAttempts = 1
	expectedRevision, expectedState := candidate.Revision, candidate.State
	candidate.Revision++
	candidate.UpdatedAt = m.now().Unix()
	if err := m.Repository.UpdateCandidateCASWithJournal(ctx, candidate, expectedRevision, expectedState, "rollback_attempted", reason, m.now()); err != nil {
		return err
	}
	m.emitAudit(ctx, candidate, "rollback_attempted", reason)
	restored, restoreErr := m.Provider.RestoreManagedDropIn(ctx, RestoreRequestV1{Fence: m.providerFence(candidate),
		ExpectedCurrentArtifactDigest: candidate.AfterArtifactDigest, Prior: prior})
	var reloaded ReloadResultV1
	var reloadErr error
	if restoreErr == nil && restored.ArtifactDigest == prior.Digest && restored.ProviderRevision == candidate.ProviderRevision && restored.ConfigurationRevision != "" {
		reloaded, reloadErr = m.Provider.ReloadSelectedService(ctx, ReloadRequestV1{Fence: m.providerFence(candidate), ArtifactDigest: prior.Digest})
	} else {
		reloadErr = domain.NewError("rollback_reload", domain.ReasonRollbackVerification)
	}
	inspected, inspectErr := m.Provider.InspectManagedDropIn(ctx, InspectRequestV1{Fence: m.providerFence(candidate)})
	observed, observeErr := m.Provider.Observe(ctx)
	postureErr := observed.Posture.Validate(m.now())
	exact := restoreErr == nil && reloadErr == nil && inspectErr == nil && observeErr == nil && postureErr == nil &&
		restored.ArtifactDigest == prior.Digest && restored.ProviderRevision == candidate.ProviderRevision && restored.ConfigurationRevision != "" &&
		reloaded.ProviderRevision == candidate.ProviderRevision && reloaded.ServiceRevision != "" && reloaded.ConfigurationRevision == restored.ConfigurationRevision &&
		inspected.ArtifactDigest == prior.Digest && inspected.Present == prior.Present && inspected.Owner == prior.Owner && inspected.Group == prior.Group &&
		inspected.ModeClass == prior.ModeClass && (prior.Mode == 0 || inspected.Mode == prior.Mode) && !inspected.Symlink && inspected.ConfigurationRevision == reloaded.ConfigurationRevision &&
		observed.ProviderRevision == candidate.ProviderRevision && observed.Posture.BinaryRevision == candidate.BinaryRevision &&
		observed.Posture.ServiceRevision == reloaded.ServiceRevision && observed.Posture.ConfigurationRevision == reloaded.ConfigurationRevision
	if !exact {
		_, err := m.advance(ctx, candidate, domain.StateManualRecoveryRequired, "rollback_verification_failed", domain.ReasonRollbackVerification)
		return errors.Join(domain.NewError("rollback", domain.ReasonRollbackVerification), restoreErr, reloadErr, inspectErr, observeErr, postureErr, err)
	}
	candidate.ServiceRevision = reloaded.ServiceRevision
	candidate.ConfigurationRevision = reloaded.ConfigurationRevision
	candidate.PostureRevision = observed.Posture.SemanticRevision
	candidate.BindingDigest = domain.BindingDigest(candidate)
	candidate.ReconciledAt = m.now().Unix()
	_, err := m.advance(ctx, candidate, domain.StateRolledBack, "rollback_verified", reason)
	return err
}

func (m *Manager) endpoints(ctx context.Context, now time.Time) []hostresources.ManagementEndpointV1 {
	if m.Endpoints == nil {
		return managementregistry.CurrentEndpoints(ctx, now)
	}
	return m.Endpoints(ctx, now)
}

func (m *Manager) providerFence(candidate domain.CandidateV1) ProviderFenceV1 {
	return ProviderFenceV1{OperationID: candidate.OperationID, CandidateRevision: candidate.Revision,
		FencingToken: domain.Revision(struct {
			Operation string
			Revision  uint64
			Binding   string
		}{candidate.OperationID, candidate.Revision, candidate.BindingDigest}),
		CandidateDigest: candidate.CandidateDigest, ExpectedProviderRevision: candidate.ProviderRevision,
		ExpectedBinaryRevision: candidate.BinaryRevision, ExpectedServiceRevision: candidate.ServiceRevision,
		ExpectedConfigurationRevision: candidate.ConfigurationRevision, DeadlineAt: m.now().Add(MaxProviderRequestDuration).Unix()}
}

func (m *Manager) evidence(ctx context.Context, now time.Time) managementregistry.EvidenceSnapshot {
	if m.Evidence == nil {
		return managementregistry.RecoveryEvidence(ctx, now)
	}
	return m.Evidence(ctx, now)
}

func (m *Manager) emitAudit(ctx context.Context, candidate domain.CandidateV1, event string, reason domain.ReasonCode) {
	if m.Audit != nil {
		m.Audit(ctx, AuditEventV1{Event: event, OperationID: candidate.OperationID, State: candidate.State, ReasonCode: reason, Revision: candidate.Revision})
	}
}

func (m *Manager) randomID(prefix string, bytes int) (string, error) {
	reader := m.Random
	if reader == nil {
		reader = rand.Reader
	}
	buffer := make([]byte, bytes)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buffer), nil
}

func previewRevision(value PreviewV1) string {
	copy := value
	copy.Revision = ""
	return domain.Revision(copy)
}

func mergeEndpoints(left, right []hostresources.ManagementEndpointV1) []hostresources.ManagementEndpointV1 {
	values := append(append([]hostresources.ManagementEndpointV1(nil), left...), right...)
	seen := map[string]bool{}
	result := make([]hostresources.ManagementEndpointV1, 0, len(values))
	for _, value := range values {
		key := value.ID + "\x00" + string(value.Family) + "\x00" + value.Bind
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func effectiveEvidence(paths []hostresources.RecoveryPathV1, endpoints []hostresources.ManagementEndpointV1, now time.Time) []hostresources.RecoveryPathV1 {
	result := make([]hostresources.RecoveryPathV1, 0, len(paths))
	for _, path := range paths {
		path = managementregistry.Effective(path, endpoints, now)
		if hostresources.RecoveryPathValid(path, now) {
			result = append(result, path)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func normalizedReasons(values []domain.ReasonCode) []domain.ReasonCode {
	seen := map[domain.ReasonCode]bool{}
	result := make([]domain.ReasonCode, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func constantDigest(value, expected string) bool {
	actualBytes, errA := hex.DecodeString(digestString(value))
	expectedBytes, errB := hex.DecodeString(expected)
	return errA == nil && errB == nil && len(actualBytes) == len(expectedBytes) && subtle.ConstantTimeCompare(actualBytes, expectedBytes) == 1
}

func safeIdentifier(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func samePolicy(left, right domain.DesiredPolicyV1) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return subtle.ConstantTimeCompare(a, b) == 1
}
