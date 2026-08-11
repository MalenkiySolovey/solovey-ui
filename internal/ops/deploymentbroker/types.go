package deploymentbroker

import domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"

const ProviderRevision = "d3ab061844280d46ccbab0fa4596430f3f496da4dd432bb1faa759c28b3cccb7"

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
