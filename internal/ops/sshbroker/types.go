// Package sshbroker owns the typed broker contract for the single Solovey SSH
// drop-in. It is deliberately narrower than a filesystem, process, or service
// abstraction.
package sshbroker

import (
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

const (
	ProviderID       = "solovey-privileged-broker/ssh/v1"
	ManagedDropIn    = "/etc/ssh/sshd_config.d/90-solovey-ui.conf"
	MainConfig       = "/etc/ssh/sshd_config"
	TicketRoot       = "/var/lib/solovey-ui-broker/ssh-proof"
	MaxDropInBytes   = 16 << 10
	ProviderRevision = "9c1a55a11564bf5d5310e3c629dc663ace3693fb62b169820655d7dff9c957bc"
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

type StageRequestV1 struct {
	ManagedContent []byte `json:"managedContent"`
}

type StageResultV1 struct {
	ArtifactDigest        string          `json:"artifactDigest"`
	Prior                 PriorArtifactV1 `json:"prior"`
	ProviderRevision      string          `json:"providerRevision"`
	ConfigurationRevision string          `json:"configurationRevision"`
}

type ValidationRequestV1 struct {
	ArtifactDigest string `json:"artifactDigest"`
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
