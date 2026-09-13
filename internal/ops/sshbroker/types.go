// Package sshbroker owns the typed broker contract for the single Solovey SSH
// drop-in. It is deliberately narrower than a filesystem, process, or service
// abstraction.
package sshbroker

import (
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const (
	ProviderID         = "solovey-privileged-broker/ssh/v1"
	ManagedDropIn      = "/etc/ssh/sshd_config.d/90-solovey-ui.conf"
	MainConfig         = "/etc/ssh/sshd_config"
	TicketRoot         = "/var/lib/solovey-ui-broker/ssh-proof"
	MaxDropInBytes     = 16 << 10
	ProviderRevision   = "5806ad0d4332084439e5bf4bdfff4480c57177d907b79dc5e5b254696d91bec9"
	MaxCheckpointBytes = 64 << 10
)

type EmptyV1 struct{}

type ObservationV1 struct {
	Posture          domain.SSHPostureV1 `json:"posture"`
	ProviderRevision string              `json:"providerRevision"`
}

type PriorArtifactV1 struct {
	Present   bool   `json:"present"`
	Content   []byte `json:"content,omitempty"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	ModeClass string `json:"modeClass"`
	Mode      uint32 `json:"mode"`
	Digest    string `json:"digest"`
}

type PrepareRequestV1 struct {
	Policy domain.DesiredPolicyV1 `json:"policy"`
}

type PreparedPolicyV1 struct {
	Implementation string `json:"implementation"`
	Format         string `json:"format"`
	Label          string `json:"label"`
	Representation string `json:"representation"`
	ArtifactDigest string `json:"artifactDigest"`
}

type StageRequestV1 struct {
	Policy                 domain.DesiredPolicyV1 `json:"policy"`
	ExpectedArtifactDigest string                 `json:"expectedArtifactDigest"`
	EndpointID             string                 `json:"endpointId"`
}

type StageResultV1 struct {
	ArtifactDigest        string          `json:"artifactDigest"`
	EndpointID            string          `json:"endpointId"`
	Prior                 PriorArtifactV1 `json:"prior"`
	ProviderRevision      string          `json:"providerRevision"`
	ConfigurationRevision string          `json:"configurationRevision"`
}

// RecoverStageRequestV1 asks only for the broker-journal result of this exact
// operation/endpoint/artifact stage. It grants no host mutation authority.
type RecoverStageRequestV1 struct {
	EndpointID             string `json:"endpointId"`
	ExpectedArtifactDigest string `json:"expectedArtifactDigest"`
}

type ReleaseStageRequestV1 struct {
	EndpointID             string `json:"endpointId"`
	ExpectedArtifactDigest string `json:"expectedArtifactDigest"`
}

type ValidationRequestV1 struct {
	ArtifactDigest string `json:"artifactDigest"`
	EndpointID     string `json:"endpointId"`
}

type ValidationResultV1 struct {
	SyntaxValid       bool                `json:"syntaxValid"`
	EffectiveValid    bool                `json:"effectiveValid"`
	EffectiveRevision string              `json:"effectiveRevision"`
	ProviderRevision  string              `json:"providerRevision"`
	ReasonCodes       []domain.ReasonCode `json:"reasonCodes,omitempty"`
}

type ReloadRequestV1 struct {
	ArtifactDigest string `json:"artifactDigest"`
	EndpointID     string `json:"endpointId"`
	Recovery       bool   `json:"recovery,omitempty"`
}

type ReloadResultV1 struct {
	ServiceRevision       string `json:"serviceRevision"`
	ConfigurationRevision string `json:"configurationRevision"`
	ProviderRevision      string `json:"providerRevision"`
}

type ArmRequestV1 struct {
	MarkerDigest        string `json:"markerDigest"`
	Verifier            string `json:"verifier"`
	EndpointID          string `json:"endpointId"`
	PrincipalID         string `json:"principalId"`
	AuthenticationClass string `json:"authenticationClass"`
	ExpiresAt           int64  `json:"expiresAt"`
}

type RestoreRequestV1 struct {
	ExpectedCurrentArtifactDigest string          `json:"expectedCurrentArtifactDigest"`
	EndpointID                    string          `json:"endpointId"`
	Prior                         PriorArtifactV1 `json:"prior"`
}

type RestoreResultV1 struct {
	ArtifactDigest        string `json:"artifactDigest"`
	ConfigurationRevision string `json:"configurationRevision"`
	ProviderRevision      string `json:"providerRevision"`
}

type InspectResultV1 struct {
	Present               bool   `json:"present"`
	ArtifactDigest        string `json:"artifactDigest"`
	Owner                 string `json:"owner"`
	Group                 string `json:"group"`
	ModeClass             string `json:"modeClass"`
	Mode                  uint32 `json:"mode"`
	Symlink               bool   `json:"symlink"`
	ConfigurationRevision string `json:"configurationRevision"`
}

type VerifyRequestV1 struct {
	MarkerDigest        string `json:"markerDigest"`
	Verifier            string `json:"verifier"`
	EndpointID          string `json:"endpointId"`
	PrincipalID         string `json:"principalId"`
	AuthenticationClass string `json:"authenticationClass"`
}

type InspectRequestV1 struct {
	EndpointID string `json:"endpointId"`
}

type VerifyResultV1 struct {
	Verified            bool   `json:"verified"`
	Independent         bool   `json:"independent"`
	FreshSession        bool   `json:"freshSession"`
	OperationBound      bool   `json:"operationBound"`
	EndpointID          string `json:"endpointId"`
	PrincipalID         string `json:"principalId"`
	AuthenticationClass string `json:"authenticationClass"`
	EvidenceRevision    string `json:"evidenceRevision"`
}

type ProofRequestV1 struct{}

// ProofResultV1 is written only to the proof AF_UNIX socket used from a fresh
// SSH session. Verifier is intentionally never used by a panel/API response.
type ProofResultV1 struct {
	OperationID string `json:"operationId"`
	Verifier    string `json:"verifier"`
	ExpiresAt   int64  `json:"expiresAt"`
}

func ValidExpiry(expires int64, now time.Time) bool {
	return expires > now.Unix() && expires <= now.Add(domain.MaxChallengeLifetime).Unix()
}
