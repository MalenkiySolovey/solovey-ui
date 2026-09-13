package deploymentbroker

import (
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// ProviderRevision binds panel requests and retained broker results to the
// checkpoint-release and crash-consistent generation contract.
const ProviderRevision = "ea9db96289e148feb094b6fa5f70b7bc9c4b22474cbe32dcb1c744b9b03cbd77"

type EmptyV1 struct{}

type ObservationV1 struct {
	Posture          domain.Posture `json:"posture"`
	ProviderRevision string         `json:"providerRevision"`
}

type DoctorResultV1 struct {
	Report           domain.DoctorReport `json:"report"`
	ProviderRevision string              `json:"providerRevision"`
}

type PrepareRequestV1 struct {
	TargetProfile domain.ProfileID `json:"targetProfile"`
}

type PrepareResultV1 struct {
	CheckpointRef    string `json:"checkpointRef"`
	ProviderRevision string `json:"providerRevision"`
}

type ReleaseCheckpointRequestV1 struct {
	CheckpointRef string `json:"checkpointRef"`
}

type ReleaseCheckpointResultV1 struct {
	CheckpointRef    string `json:"checkpointRef"`
	ProviderRevision string `json:"providerRevision"`
}

type ApplyRequestV1 struct {
	TargetProfile domain.ProfileID `json:"targetProfile"`
	CheckpointRef string           `json:"checkpointRef"`
}

type ApplyResultV1 struct {
	TargetProfile    domain.ProfileID `json:"targetProfile"`
	ProviderRevision string           `json:"providerRevision"`
}

type VerifyRequestV1 struct {
	TargetProfile domain.ProfileID `json:"targetProfile"`
	CheckpointRef string           `json:"checkpointRef"`
}

type VerifyResultV1 struct {
	Verified         bool           `json:"verified"`
	Posture          domain.Posture `json:"posture"`
	ProviderRevision string         `json:"providerRevision"`
}

type RollbackRequestV1 struct {
	FromProfile   domain.ProfileID `json:"fromProfile"`
	CheckpointRef string           `json:"checkpointRef"`
}

type RollbackResultV1 struct {
	Verified         bool           `json:"verified"`
	Posture          domain.Posture `json:"posture"`
	ProviderRevision string         `json:"providerRevision"`
}

func completedMutationLifecycle(verb broker.Verb) broker.CompletionPolicy {
	switch verb {
	case broker.VerbDeploymentPrepare:
		return broker.CompletionPolicy{RetainUntilRelease: true}
	case broker.VerbDeploymentRelease:
		return broker.CompletionPolicy{ReleasesVerb: broker.VerbDeploymentPrepare}
	default:
		return broker.CompletionPolicy{}
	}
}
