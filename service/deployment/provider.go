package deployment

import (
	"context"
	"os"
	"strings"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

const MaxProviderDuration = 2 * time.Minute

type FenceV1 struct {
	OperationID     string
	Revision        uint64
	Token           string
	ExpectedPosture string
	DeadlineAt      int64
}

type ManagementPreservation struct {
	Ready            bool     `json:"ready"`
	EvidenceRevision string   `json:"evidenceRevision"`
	Revision         string   `json:"revision"`
	Reasons          []string `json:"reasons,omitempty"`
}

type RuntimeHealth struct {
	Ready            bool     `json:"ready"`
	EvidenceRevision string   `json:"evidenceRevision"`
	Revision         string   `json:"revision"`
	Reasons          []string `json:"reasons,omitempty"`
}

type BrokerPresentation struct {
	Available        bool   `json:"available"`
	ProtocolRevision string `json:"protocolRevision"`
	Transport        string `json:"transport"`
	PeerPosture      string `json:"peerPosture"`
}

// UpdatePresentation contains released deployment-specific display aliases.
// It never selects update lifecycle authority; UpdateLifecycle is the sole
// semantic projection consumed by portable Update.
type UpdatePresentation struct {
	LegacyMode        string
	LegacyReasonCodes []string
}

type brokerPresenter interface {
	BrokerPresentation(context.Context) BrokerPresentation
}

type updatePresenter interface {
	UpdatePresentation() UpdatePresentation
}

var runtimeProviderFactories = map[string]func() Provider{}

func registerRuntimeProvider(kind string, factory func() Provider) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" || factory == nil {
		panic("deployment runtime provider registration is invalid")
	}
	if _, exists := runtimeProviderFactories[kind]; exists {
		panic("deployment runtime provider is registered twice: " + kind)
	}
	runtimeProviderFactories[kind] = factory
}

func RuntimeProvider() Provider {
	kind := strings.ToLower(strings.TrimSpace(os.Getenv("SUI_DEPLOYMENT_KIND")))
	if factory := runtimeProviderFactories[kind]; factory != nil {
		return factory()
	}
	return UnavailableProvider{}
}

type Provider interface {
	ProviderID() string
	Capabilities(context.Context) domain.Capabilities
	Observe(context.Context) (domain.Posture, error)
	Doctor(context.Context) (domain.DoctorReport, error)
	Prepare(context.Context, FenceV1, domain.ProfileID) (string, error)
	Apply(context.Context, FenceV1, domain.ProfileID, string) error
	Verify(context.Context, FenceV1, domain.ProfileID, string) (domain.Posture, error)
	Rollback(context.Context, FenceV1, domain.ProfileID, string) (domain.Posture, error)
}

// CheckpointLifecycle is the narrow cross-store contract required before a
// deployment provider may create rollback authority. Recovery must return the
// exact completed Prepare result without a second host mutation. Release is
// idempotent and is called only after the panel has committed a safe terminal
// operation.
type CheckpointLifecycle interface {
	RecoverPreparedCheckpoint(context.Context, FenceV1, domain.ProfileID) (string, error)
	ReleaseCheckpoint(context.Context, FenceV1, string) error
}

type UpdateLifecycle string

const (
	UpdateLifecycleSelfManaged     UpdateLifecycle = "self-managed"
	UpdateLifecyclePackageManaged  UpdateLifecycle = "package-managed"
	UpdateLifecycleOperatorManaged UpdateLifecycle = "operator-managed"
	UpdateLifecycleUnavailable     UpdateLifecycle = "unavailable"
)

type updateLifecycleProvider interface {
	UpdateLifecycle() UpdateLifecycle
}

// RuntimeUpdateLifecycle projects the already-selected deployment provider
// into the semantic fact consumed by portable Update. Update never repeats
// deployment environment or platform detection.
func RuntimeUpdateLifecycle() UpdateLifecycle {
	provider := RuntimeProvider()
	projector, ok := provider.(updateLifecycleProvider)
	if !ok {
		return UpdateLifecycleUnavailable
	}
	return projector.UpdateLifecycle()
}

type UnavailableProvider struct{}

func (UnavailableProvider) ProviderID() string { return "deployment-provider-unavailable" }
func (UnavailableProvider) UpdateLifecycle() UpdateLifecycle {
	return UpdateLifecycleUnavailable
}
func (UnavailableProvider) Capabilities(context.Context) domain.Capabilities {
	result := domain.Capabilities{Observe: domain.Unavailable, Doctor: domain.Unavailable, Migrate: domain.Unavailable,
		Rollback: domain.Unavailable, Reasons: []string{"privileged_broker_unavailable"}}
	result.Revision = domain.Revision(result)
	return result
}
func (UnavailableProvider) Observe(context.Context) (domain.Posture, error) {
	return domain.Posture{}, ErrProviderUnavailable
}
func (UnavailableProvider) Doctor(context.Context) (domain.DoctorReport, error) {
	return domain.DoctorReport{}, ErrProviderUnavailable
}
func (UnavailableProvider) Prepare(context.Context, FenceV1, domain.ProfileID) (string, error) {
	return "", ErrProviderUnavailable
}
func (UnavailableProvider) Apply(context.Context, FenceV1, domain.ProfileID, string) error {
	return ErrProviderUnavailable
}
func (UnavailableProvider) Verify(context.Context, FenceV1, domain.ProfileID, string) (domain.Posture, error) {
	return domain.Posture{}, ErrProviderUnavailable
}
func (UnavailableProvider) Rollback(context.Context, FenceV1, domain.ProfileID, string) (domain.Posture, error) {
	return domain.Posture{}, ErrProviderUnavailable
}

func (UnavailableProvider) BrokerPresentation(context.Context) BrokerPresentation {
	return BrokerPresentation{Transport: "unavailable", PeerPosture: "unavailable"}
}
