package sshmanagement

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const MaxProviderRequestDuration = 5 * time.Second

// Provider is deliberately narrower than a command/file/service API. Each
// method represents one semantic step for the single managed SSH drop-in.
type Provider interface {
	ProviderID() string
	Capabilities(context.Context) domain.CapabilitySetV1
	Observe(context.Context) (ObservationV1, error)
	StageManagedDropIn(context.Context, StageRequestV1) (StageResultV1, error)
	ValidateManagedDropIn(context.Context, ValidationRequestV1) (ValidationResultV1, error)
	ReloadSelectedService(context.Context, ReloadRequestV1) (ReloadResultV1, error)
	VerifyReconnect(context.Context, ReconnectProofV1) (ReconnectResultV1, error)
	RestoreManagedDropIn(context.Context, RestoreRequestV1) (RestoreResultV1, error)
	InspectManagedDropIn(context.Context, InspectRequestV1) (InspectResultV1, error)
}

// ReconnectArmer is the production out-of-band reconnect extension. Providers use
// it to move the one-time verifier into root-owned broker state; the HTTP
// layer continues to omit the verifier entirely.
type ReconnectArmer interface {
	ArmReconnect(context.Context, ReconnectProofV1, int64) error
}

type ObservationV1 struct {
	Posture          domain.SSHPostureV1 `json:"posture"`
	ProviderRevision string              `json:"providerRevision"`
}

// ProviderFenceV1 is the common authority envelope for every provider action.
// Its identities are semantic revisions, not caller-selected paths, binaries,
// services, commands, arguments or environment.
type ProviderFenceV1 struct {
	OperationID                   string
	CandidateRevision             uint64
	FencingToken                  string
	CandidateDigest               string
	ExpectedProviderRevision      string
	ExpectedBinaryRevision        string
	ExpectedServiceRevision       string
	ExpectedConfigurationRevision string
	DeadlineAt                    int64
}

func (f ProviderFenceV1) Validate(now time.Time) error {
	deadline := time.Unix(f.DeadlineAt, 0).UTC()
	if !safeIdentifier(f.OperationID, 64) || f.CandidateRevision == 0 || !providerDigest(f.FencingToken) ||
		!providerDigest(f.CandidateDigest) || !providerDigest(f.ExpectedProviderRevision) || !providerDigest(f.ExpectedBinaryRevision) ||
		!providerDigest(f.ExpectedServiceRevision) || !providerDigest(f.ExpectedConfigurationRevision) ||
		!deadline.After(now.UTC().Add(-time.Second)) || deadline.After(now.UTC().Add(MaxProviderRequestDuration)) {
		return domain.NewError("provider_fence", domain.ReasonRevisionMismatch)
	}
	return nil
}

func providerDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type PriorArtifactV1 struct {
	Present   bool   `json:"present"`
	Content   []byte `json:"-"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	ModeClass string `json:"modeClass"`
	Mode      uint32 `json:"mode"`
	Digest    string `json:"digest"`
}

type StageRequestV1 struct {
	Fence          ProviderFenceV1
	ManagedContent []byte
}

type StageResultV1 struct {
	ArtifactDigest        string
	Prior                 PriorArtifactV1
	ProviderRevision      string
	ConfigurationRevision string
}

type ValidationRequestV1 struct {
	Fence          ProviderFenceV1
	ArtifactDigest string
}

type ValidationResultV1 struct {
	SyntaxValid       bool
	EffectiveValid    bool
	EffectiveRevision string
	ProviderRevision  string
	ReasonCodes       []domain.ReasonCode
}

type ReloadRequestV1 struct {
	Fence          ProviderFenceV1
	ArtifactDigest string
}

type ReloadResultV1 struct {
	ServiceRevision       string
	ConfigurationRevision string
	ProviderRevision      string
}

type ReconnectProofV1 struct {
	Fence               ProviderFenceV1
	MarkerDigest        string
	Verifier            string
	EndpointID          string
	PrincipalID         string
	AuthenticationClass string
}

type ReconnectResultV1 struct {
	Verified            bool
	Independent         bool
	FreshSession        bool
	OperationBound      bool
	EndpointID          string
	PrincipalID         string
	AuthenticationClass string
	EvidenceRevision    string
}

type RestoreRequestV1 struct {
	Fence                         ProviderFenceV1
	ExpectedCurrentArtifactDigest string
	Prior                         PriorArtifactV1
}

type RestoreResultV1 struct {
	ArtifactDigest        string
	ConfigurationRevision string
	ProviderRevision      string
}

type InspectRequestV1 struct {
	Fence ProviderFenceV1
}

type InspectResultV1 struct {
	Present               bool
	ArtifactDigest        string
	Owner                 string
	Group                 string
	ModeClass             string
	Mode                  uint32
	Symlink               bool
	ConfigurationRevision string
}

type UnavailableProvider struct{}

func (UnavailableProvider) ProviderID() string { return "production-unavailable" }
func (UnavailableProvider) Capabilities(context.Context) domain.CapabilitySetV1 {
	result := domain.CapabilitySetV1{
		ObservePosture: domain.AvailabilityUnavailable, Prepare: domain.AvailabilityUnavailable,
		Stage: domain.AvailabilityUnavailable, Validate: domain.AvailabilityUnavailable,
		Reload: domain.AvailabilityUnavailable, Reconnect: domain.AvailabilityUnavailable,
		Rollback:    domain.AvailabilityUnavailable,
		ReasonCodes: []domain.ReasonCode{domain.ReasonProductionMutationAbsent},
	}
	result.Revision = domain.Revision(result)
	return result
}

func unavailable(operation string) error {
	return domain.NewError(operation, domain.ReasonProviderUnavailable)
}

func (UnavailableProvider) Observe(context.Context) (ObservationV1, error) {
	return ObservationV1{}, unavailable("observe")
}
func (UnavailableProvider) StageManagedDropIn(context.Context, StageRequestV1) (StageResultV1, error) {
	return StageResultV1{}, unavailable("stage")
}
func (UnavailableProvider) ValidateManagedDropIn(context.Context, ValidationRequestV1) (ValidationResultV1, error) {
	return ValidationResultV1{}, unavailable("validate")
}
func (UnavailableProvider) ReloadSelectedService(context.Context, ReloadRequestV1) (ReloadResultV1, error) {
	return ReloadResultV1{}, unavailable("reload")
}
func (UnavailableProvider) VerifyReconnect(context.Context, ReconnectProofV1) (ReconnectResultV1, error) {
	return ReconnectResultV1{}, unavailable("reconnect")
}
func (UnavailableProvider) RestoreManagedDropIn(context.Context, RestoreRequestV1) (RestoreResultV1, error) {
	return RestoreResultV1{}, unavailable("restore")
}
func (UnavailableProvider) InspectManagedDropIn(context.Context, InspectRequestV1) (InspectResultV1, error) {
	return InspectResultV1{}, unavailable("inspect")
}
