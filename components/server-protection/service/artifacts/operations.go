package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// OperationRecovery connects persisted journal facts to local artifacts. It is
// the mutation-inert fallback used when no restricted per-kind rollback backend
// is available; production composition replaces it for firewall and fronting.
type OperationRecovery struct {
	Storage    *Storage
	Repository OperationRecoveryRepository
}

type OperationRecoveryRepository interface {
	ArtifactByOperation(context.Context, string) (protectionrepository.ArtifactModel, error)
	SaveArtifact(context.Context, *protectionrepository.ArtifactModel) error
}

type frontingRecoveryEvidence struct {
	Version                    int      `json:"version"`
	Schema                     string   `json:"schema,omitempty"`
	OperationID                string   `json:"operationId"`
	ArtifactRevision           string   `json:"artifactRevision"`
	ArtifactManifestSHA256     string   `json:"artifactManifestSha256,omitempty"`
	DesiredRevision            string   `json:"desiredRevision"`
	CandidateRevision          string   `json:"candidateRevision,omitempty"`
	CandidateSHA256            string   `json:"candidateSha256"`
	PreviousRevision           string   `json:"previousRevision"`
	PreviousSHA256             string   `json:"previousSha256"`
	RuntimeIdentityRevision    string   `json:"runtimeIdentityRevision,omitempty"`
	StrategyCapabilityRevision string   `json:"strategyCapabilityRevision,omitempty"`
	SocketClaimRevision        string   `json:"socketClaimRevision,omitempty"`
	BackendReferenceRevision   string   `json:"backendReferenceRevision,omitempty"`
	BackendReferenceRevisions  []string `json:"backendReferenceRevisions,omitempty"`
	SelectorSetRevision        string   `json:"selectorSetRevision,omitempty"`
	MapRevision                string   `json:"mapRevision,omitempty"`
	UpstreamIDSetRevision      string   `json:"upstreamIdSetRevision,omitempty"`
	ExpectedActiveRevision     string   `json:"expectedActiveRevision,omitempty"`
	ActualActiveRevision       string   `json:"actualActiveRevision,omitempty"`
	ProcessIdentityRevision    string   `json:"processIdentityRevision,omitempty"`
	ListenerRevision           string   `json:"listenerVerificationRevision,omitempty"`
	RollbackAttemptCount       int      `json:"rollbackAttemptCount,omitempty"`
	FailedStage                string   `json:"failedStage,omitempty"`
	Plan                       struct {
		CanonicalPlanDigest string `json:"canonicalPlanDigest"`
		Strategy            struct {
			Selected string `json:"selected"`
		} `json:"strategy"`
	} `json:"plan,omitempty"`
	Lease struct {
		LeaseID       string `json:"leaseId"`
		LeaseRevision string `json:"leaseRevision"`
		State         string `json:"state"`
	} `json:"lease,omitempty"`
	EndpointLeases []struct {
		LeaseID       string `json:"leaseId"`
		LeaseRevision string `json:"leaseRevision"`
		State         string `json:"state"`
	} `json:"endpointLeases,omitempty"`
	FallbackReservations []struct {
		ReservationID       string `json:"reservationId"`
		ReservationRevision string `json:"reservationRevision"`
		State               string `json:"state"`
	} `json:"fallbackReservations,omitempty"`
}

type firewallRecoveryEvidence struct {
	Version              int    `json:"version"`
	OperationID          string `json:"operationId"`
	ArtifactRevision     string `json:"artifactRevision"`
	PlanRevision         string `json:"planRevision"`
	CandidateSHA256      string `json:"candidateSha256"`
	RollbackSHA256       string `json:"rollbackSha256"`
	PreviousRevision     string `json:"previousRevision,omitempty"`
	PreviousTablePresent bool   `json:"previousTablePresent"`
}

func (r OperationRecovery) HasMutationArtifact(ctx context.Context, operation protectionrepository.OperationLockModel) (bool, error) {
	if r.Storage == nil || r.Repository == nil || !r.Storage.HasMutationMarker(operation.OperationID) {
		return false, nil
	}
	artifact, err := r.Repository.ArtifactByOperation(ctx, operation.OperationID)
	if err != nil {
		if errors.Is(err, protectionrepository.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	_, err = r.Storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256)
	return err == nil, err
}

func (OperationRecovery) AttemptRollback(context.Context, protectionrepository.OperationLockModel) error {
	return errors.New("automatic rollback backend is not wired")
}

func (r OperationRecovery) CreateBundle(ctx context.Context, operation protectionrepository.OperationLockModel, state string) error {
	if r.Storage == nil || r.Repository == nil {
		return errors.New("artifact recovery is not initialized")
	}
	artifact, err := r.Repository.ArtifactByOperation(ctx, operation.OperationID)
	if err != nil {
		return err
	}
	health := []HealthCheck{}
	var healthFacts struct {
		Checks []HealthCheck `json:"checks"`
	}
	if len(operation.RecoveryErrorCode) > 0 {
		healthFacts.Checks = append(healthFacts.Checks, HealthCheck{ID: "automatic_rollback", Status: "failed", FactCode: "rollback_backend_unavailable"})
	}
	if data, marshalErr := json.Marshal(healthFacts); marshalErr == nil && len(data) > 0 {
		health = healthFacts.Checks
	}
	var frontingEvidence frontingRecoveryEvidence
	var firewallEvidence firewallRecoveryEvidence
	if operation.Kind == "fronting" {
		data, stateErr := r.Storage.ReadFrontingState(operation.OperationID)
		if stateErr != nil {
			return errors.Join(errors.New("fronting recovery checkpoint is unavailable"), stateErr)
		}
		if err := json.Unmarshal(data, &frontingEvidence); err != nil {
			return errors.Join(errors.New("fronting recovery checkpoint is invalid"), err)
		}
		if (frontingEvidence.Version != 1 && frontingEvidence.Version != 2) || frontingEvidence.OperationID != operation.OperationID || frontingEvidence.ArtifactRevision != artifact.Revision ||
			safeRevisionReference(frontingDesiredRevision(frontingEvidence)) != frontingDesiredRevision(frontingEvidence) || safeSHA256(frontingEvidence.CandidateSHA256) != frontingEvidence.CandidateSHA256 ||
			safeRevisionReference(frontingEvidence.PreviousRevision) != frontingEvidence.PreviousRevision || safeSHA256(frontingEvidence.PreviousSHA256) != frontingEvidence.PreviousSHA256 {
			return errors.New("fronting recovery checkpoint identity is invalid")
		}
		manifest, verifyErr := r.Storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256)
		if verifyErr == nil {
			if manifest.OperationID != operation.OperationID {
				return errors.New("fronting recovery artifact identity is invalid")
			}
			if err := validateFrontingRecoveryEvidence(r.Storage, artifact.Revision, manifest, frontingEvidence); err != nil {
				return err
			}
		}
	}
	if operation.Kind == "firewall" {
		firewallEvidence, err = loadFirewallRecoveryEvidence(r.Storage, operation, artifact)
		if err != nil {
			return err
		}
	}
	desiredRevision := frontingDesiredRevision(frontingEvidence)
	candidateSHA := frontingEvidence.CandidateSHA256
	previousRevision := frontingEvidence.PreviousRevision
	previousSHA := frontingEvidence.PreviousSHA256
	rollbackSHA := ""
	previousTablePresent := false
	if operation.Kind == "firewall" {
		desiredRevision = firewallEvidence.PlanRevision
		candidateSHA = firewallEvidence.CandidateSHA256
		previousRevision = firewallEvidence.PreviousRevision
		rollbackSHA = firewallEvidence.RollbackSHA256
		previousTablePresent = firewallEvidence.PreviousTablePresent
		if previousTablePresent {
			previousSHA = rollbackSHA
		}
	}
	targetAuthorities := make([]TargetAuthorityRecoveryInput, 0, len(frontingEvidence.EndpointLeases)+len(frontingEvidence.FallbackReservations))
	for _, lease := range frontingEvidence.EndpointLeases {
		targetAuthorities = append(targetAuthorities, TargetAuthorityRecoveryInput{Kind: "endpoint_lease", ID: lease.LeaseID, Revision: lease.LeaseRevision, State: lease.State})
	}
	for _, reservation := range frontingEvidence.FallbackReservations {
		targetAuthorities = append(targetAuthorities, TargetAuthorityRecoveryInput{Kind: "fallback_reservation", ID: reservation.ReservationID, Revision: reservation.ReservationRevision, State: reservation.State})
	}
	bundle, err := r.Storage.CreateRecoveryBundle(RecoveryInput{
		OperationID: operation.OperationID, Revision: artifact.Revision, State: state,
		DesiredRevision: desiredRevision, CandidateSHA256: candidateSHA,
		PreviousRevision: previousRevision, PreviousSHA256: previousSHA, RollbackSHA256: rollbackSHA, PreviousTablePresent: previousTablePresent, ArtifactManifestSHA256: artifact.ManifestSHA256,
		ResourceID: operation.ResourceID, ResourceKind: operation.Kind, Protocol: operation.Protocol, Listen: operation.Listen,
		Port: valueOrZero(operation.Port), FromOwner: "previous_owner", ToOwner: "planned_owner",
		CreatedAt: operation.CreatedAt, UpdatedAt: time.Now().Unix(), Health: health,
		Strategy: frontingEvidence.Plan.Strategy.Selected, PlanDigest: frontingEvidence.Plan.CanonicalPlanDigest,
		SocketClaimRevision: frontingEvidence.SocketClaimRevision, BackendReferenceRevision: frontingEvidence.BackendReferenceRevision,
		BackendReferenceRevisions: frontingEvidence.BackendReferenceRevisions, SelectorSetRevision: frontingEvidence.SelectorSetRevision,
		MapRevision: frontingEvidence.MapRevision, UpstreamIDSetRevision: frontingEvidence.UpstreamIDSetRevision, TargetAuthorities: targetAuthorities,
		ProviderLeaseID: frontingEvidence.Lease.LeaseID, ProviderLeaseRevision: frontingEvidence.Lease.LeaseRevision,
		ProviderLeaseState: frontingEvidence.Lease.State, ExpectedActiveRevision: frontingEvidence.ExpectedActiveRevision,
		ActualActiveRevision: frontingEvidence.ActualActiveRevision, ProcessRevision: frontingEvidence.ProcessIdentityRevision,
		ListenerRevision: frontingEvidence.ListenerRevision, FailedStage: frontingEvidence.FailedStage,
		RollbackAttemptCount: frontingEvidence.RollbackAttemptCount, PermittedNextAction: "inspect_and_reconcile",
	}, artifact.ManifestSHA256)
	if err != nil {
		bundle, err = r.Storage.CreateEmergencyRecoveryBundle(RecoveryInput{
			OperationID: operation.OperationID, Revision: artifact.Revision, State: state,
			DesiredRevision: desiredRevision, CandidateSHA256: candidateSHA,
			PreviousRevision: previousRevision, PreviousSHA256: previousSHA, RollbackSHA256: rollbackSHA, PreviousTablePresent: previousTablePresent, ArtifactManifestSHA256: artifact.ManifestSHA256,
			ResourceID: operation.ResourceID, ResourceKind: operation.Kind, Protocol: operation.Protocol, Listen: operation.Listen,
			Port: valueOrZero(operation.Port), FromOwner: "previous_owner", ToOwner: "planned_owner",
			CreatedAt: operation.CreatedAt, UpdatedAt: time.Now().Unix(),
		}, "checksum_mismatch")
	}
	if err != nil {
		return err
	}
	artifact.Scope = "recovery"
	artifact.UpdatedAt = time.Now().Unix()
	artifact.Bytes += bundle.Bytes
	return r.Repository.SaveArtifact(ctx, &artifact)
}

var firewallRevisionMarker = regexp.MustCompile(`comment "solovey-revision:([0-9a-f]{64})"`)

func loadFirewallRecoveryEvidence(storage *Storage, operation protectionrepository.OperationLockModel, artifact protectionrepository.ArtifactModel) (firewallRecoveryEvidence, error) {
	manifest, err := storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256)
	if err != nil || manifest.OperationID != operation.OperationID {
		return firewallRecoveryEvidence{}, errors.Join(errors.New("firewall recovery artifact identity is invalid"), err)
	}
	candidate, err := readVerifiedFrontingArtifactFile(storage, artifact.Revision, manifest, "candidate.nft")
	if err != nil {
		return firewallRecoveryEvidence{}, errors.Join(errors.New("firewall recovery candidate identity is unavailable"), err)
	}
	metadata, err := readVerifiedFrontingArtifactFile(storage, artifact.Revision, manifest, "managed-table.json")
	if err != nil {
		return firewallRecoveryEvidence{}, errors.Join(errors.New("firewall recovery plan identity is unavailable"), err)
	}
	var managed struct {
		PlanRevision string `json:"plan_revision"`
	}
	if json.Unmarshal(metadata, &managed) != nil || safeSHA256(managed.PlanRevision) != managed.PlanRevision || managed.PlanRevision != operation.PlanRevision {
		return firewallRecoveryEvidence{}, errors.New("firewall recovery plan revision does not match the fenced operation")
	}
	markers := firewallRevisionMarker.FindAllSubmatch(candidate, -1)
	if len(markers) != 1 || string(markers[0][1]) != managed.PlanRevision {
		return firewallRecoveryEvidence{}, errors.New("firewall recovery candidate revision marker is invalid")
	}
	rollbackSHA, err := storage.ReadFirewallRollbackEvidence(artifact.Revision)
	if err != nil {
		return firewallRecoveryEvidence{}, errors.Join(errors.New("firewall recovery rollback identity is unavailable"), err)
	}
	rollbackPath, err := storage.resolve(filepathSlash("revisions", artifact.Revision, "firewall-before.nft"), true)
	if err != nil {
		return firewallRecoveryEvidence{}, err
	}
	rollback, err := os.ReadFile(rollbackPath)
	if err != nil || len(rollback) == 0 || len(rollback) > 512<<10 || digestArtifact(rollback) != rollbackSHA {
		return firewallRecoveryEvidence{}, errors.Join(errors.New("firewall recovery rollback artifact is invalid"), err)
	}
	previousPresent := strings.TrimSpace(string(rollback)) != "delete table inet solovey_protection"
	previousRevision := ""
	if previousPresent {
		previousMarkers := firewallRevisionMarker.FindAllSubmatch(rollback, -1)
		if len(previousMarkers) != 1 {
			return firewallRecoveryEvidence{}, errors.New("firewall recovery previous revision is not exact")
		}
		previousRevision = string(previousMarkers[0][1])
	}
	evidence := firewallRecoveryEvidence{Version: 1, OperationID: operation.OperationID, ArtifactRevision: artifact.Revision, PlanRevision: managed.PlanRevision, CandidateSHA256: digestArtifact(candidate), RollbackSHA256: rollbackSHA, PreviousRevision: previousRevision, PreviousTablePresent: previousPresent}
	stateData, stateErr := storage.ReadFirewallState(operation.OperationID)
	if stateErr == nil {
		var persisted firewallRecoveryEvidence
		if json.Unmarshal(stateData, &persisted) != nil || persisted != evidence {
			return firewallRecoveryEvidence{}, errors.New("firewall recovery checkpoint does not match exact artifacts")
		}
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		return firewallRecoveryEvidence{}, errors.Join(errors.New("firewall recovery checkpoint is invalid"), stateErr)
	}
	return evidence, nil
}

func validateFrontingRecoveryEvidence(storage *Storage, revision string, manifest Manifest, evidence frontingRecoveryEvidence) error {
	candidate, err := readVerifiedFrontingArtifactFile(storage, revision, manifest, "candidate.conf")
	if err != nil || digestArtifact(candidate) != evidence.CandidateSHA256 {
		return errors.Join(errors.New("fronting recovery candidate identity does not match artifact"), err)
	}
	if evidence.Version == 1 {
		canonical, err := readVerifiedFrontingArtifactFile(storage, revision, manifest, "canonical.json")
		if err != nil || digestArtifact([]byte(trimArtifactNewline(string(canonical)))) != evidence.DesiredRevision {
			return errors.Join(errors.New("fronting recovery desired revision does not match artifact"), err)
		}
	} else {
		if evidence.Schema != "solovey-ui/fronting-workflow-checkpoint/v2" || evidence.CandidateRevision != revision ||
			evidence.ExpectedActiveRevision != evidence.CandidateRevision || safeSHA256(evidence.Plan.CanonicalPlanDigest) != evidence.Plan.CanonicalPlanDigest ||
			evidence.ArtifactManifestSHA256 != manifestSHA(manifest) && evidence.ArtifactManifestSHA256 != "" {
			return errors.New("fronting recovery v2 checkpoint identity is invalid")
		}
		plan, planErr := readVerifiedFrontingArtifactFile(storage, revision, manifest, "canonical-plan.json")
		binding, bindingErr := readVerifiedFrontingArtifactFile(storage, revision, manifest, "candidate-binding.json")
		if planErr != nil || bindingErr != nil || len(plan) == 0 || len(binding) == 0 {
			return errors.New("fronting recovery v2 bindings are unavailable")
		}
	}
	rollbackData, err := readVerifiedFrontingArtifactFile(storage, revision, manifest, "rollback.json")
	if err != nil {
		return errors.Join(errors.New("fronting recovery rollback identity is unavailable"), err)
	}
	var rollback struct {
		Revision string `json:"revision"`
		SHA256   string `json:"sha256"`
	}
	if json.Unmarshal(rollbackData, &rollback) != nil || rollback.Revision != evidence.PreviousRevision || rollback.SHA256 != evidence.PreviousSHA256 {
		return errors.New("fronting recovery previous revision does not match artifact")
	}
	return nil
}

func frontingDesiredRevision(evidence frontingRecoveryEvidence) string {
	if evidence.Version == 2 {
		return evidence.CandidateRevision
	}
	return evidence.DesiredRevision
}

func manifestSHA(manifest Manifest) string {
	payload, _ := json.MarshalIndent(manifest, "", "  ")
	payload = append(payload, '\n')
	return digestArtifact(payload)
}

func readVerifiedFrontingArtifactFile(storage *Storage, revision string, manifest Manifest, name string) ([]byte, error) {
	var expected *File
	for index := range manifest.Files {
		if manifest.Files[index].Path == name {
			expected = &manifest.Files[index]
			break
		}
	}
	if expected == nil || expected.Bytes < 0 || expected.Bytes > 512<<10 {
		return nil, errors.New("fronting recovery artifact file is missing or unbounded")
	}
	path, err := storage.resolve(filepathSlash("revisions", revision, name), true)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != expected.Bytes || digestArtifact(data) != expected.SHA256 {
		return nil, ErrChecksumMismatch
	}
	return data, nil
}

func digestArtifact(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func trimArtifactNewline(value string) string {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		return value[:len(value)-1]
	}
	return value
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
