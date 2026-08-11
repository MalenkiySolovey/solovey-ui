package deployment

import (
	"context"
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

type UnavailableProvider struct{}

func (UnavailableProvider) ProviderID() string { return "deployment-provider-unavailable" }
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
