// Package updatebroker defines the narrow panel-to-root release activation
// protocol. It contains semantic release identities and bounded chunks, never
// commands, paths, services, repositories, package-manager inputs or URLs.
package updatebroker

const (
	ProviderRevision = "update-broker-provider-1.1"
	MaxChunkBytes    = 384 << 10
)

type EmptyV1 struct{}

type ArtifactIdentityV1 struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	Platform   string `json:"platform"`
	Arch       string `json:"arch"`
	MediaType  string `json:"mediaType"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	Provenance string `json:"provenance"`
}

type ReleaseIdentityV1 struct {
	ReleaseID          string               `json:"releaseId"`
	Sequence           uint64               `json:"sequence"`
	Version            string               `json:"version"`
	ManifestDigest     string               `json:"manifestDigest"`
	ArtifactSetDigest  string               `json:"artifactSetDigest"`
	BinaryProfile      string               `json:"binaryProfile"`
	DeploymentRevision string               `json:"deploymentRevision"`
	MigrationSetDigest string               `json:"migrationSetDigest"`
	RestartClass       string               `json:"restartClass"`
	RollbackClass      string               `json:"rollbackClass"`
	Artifacts          []ArtifactIdentityV1 `json:"artifacts"`
}

type ObservationV1 struct {
	ProviderRevision               string `json:"providerRevision"`
	InstalledSequence              uint64 `json:"installedSequence"`
	ActiveSequence                 uint64 `json:"activeSequence"`
	VerifiedSequence               uint64 `json:"verifiedSequence"`
	MaxActivatedSequence           uint64 `json:"maxActivatedSequence"`
	InstalledDigest                string `json:"installedDigest,omitempty"`
	ActiveDigest                   string `json:"activeDigest,omitempty"`
	VerifiedDigest                 string `json:"verifiedDigest,omitempty"`
	MaxActivatedDigest             string `json:"maxActivatedDigest,omitempty"`
	RollbackAvailable              bool   `json:"rollbackAvailable"`
	RollbackOperationID            string `json:"rollbackOperationId,omitempty"`
	RollbackManifestDigest         string `json:"rollbackManifestDigest,omitempty"`
	RollbackRef                    string `json:"rollbackRef,omitempty"`
	RollbackTargetSequence         uint64 `json:"rollbackTargetSequence,omitempty"`
	RollbackTargetDigest           string `json:"rollbackTargetDigest,omitempty"`
	PreparedRollbackAvailable      bool   `json:"preparedRollbackAvailable"`
	PreparedRollbackOperationID    string `json:"preparedRollbackOperationId,omitempty"`
	PreparedRollbackManifestDigest string `json:"preparedRollbackManifestDigest,omitempty"`
	PreparedRollbackRef            string `json:"preparedRollbackRef,omitempty"`
	PreparedRollbackTargetSequence uint64 `json:"preparedRollbackTargetSequence,omitempty"`
	PreparedRollbackTargetDigest   string `json:"preparedRollbackTargetDigest,omitempty"`
	ManagementReady                bool   `json:"managementReady"`
	ObservedAt                     int64  `json:"observedAt"`
	Revision                       string `json:"revision"`
}

type StageChunkRequestV1 struct {
	Release        ReleaseIdentityV1  `json:"release"`
	Artifact       ArtifactIdentityV1 `json:"artifact"`
	Offset         int64              `json:"offset"`
	Chunk          []byte             `json:"chunk"`
	Final          bool               `json:"final"`
	ExpectedPrefix string             `json:"expectedPrefix,omitempty"`
}

type StageChunkResultV1 struct {
	ProviderRevision string `json:"providerRevision"`
	AcceptedBytes    int64  `json:"acceptedBytes"`
	Complete         bool   `json:"complete"`
	ArtifactDigest   string `json:"artifactDigest,omitempty"`
}

type ReleaseStagingRequestV1 struct {
	ManifestDigest string `json:"manifestDigest"`
}

type ReleaseStagingResultV1 struct {
	ProviderRevision string `json:"providerRevision"`
	Released         bool   `json:"released"`
}

type PrepareRequestV1 struct {
	Release                    ReleaseIdentityV1 `json:"release"`
	ExpectedBrokerCapability   string            `json:"expectedBrokerCapability"`
	ExpectedManagementRevision string            `json:"expectedManagementRevision"`
}

type PrepareResultV1 struct {
	ProviderRevision  string `json:"providerRevision"`
	PreparedRef       string `json:"preparedRef"`
	RollbackRef       string `json:"rollbackRef"`
	RollbackAvailable bool   `json:"rollbackAvailable"`
	ManagementReady   bool   `json:"managementReady"`
	PreflightRevision string `json:"preflightRevision"`
}

type ActivateRequestV1 struct {
	Release      ReleaseIdentityV1 `json:"release"`
	PreparedRef  string            `json:"preparedRef"`
	RollbackRef  string            `json:"rollbackRef"`
	ExpectedMode string            `json:"expectedMode"`
}

type ActivateResultV1 struct {
	ProviderRevision string `json:"providerRevision"`
	ActiveSequence   uint64 `json:"activeSequence"`
	ActiveDigest     string `json:"activeDigest"`
	RestartRequired  bool   `json:"restartRequired"`
}

type VerifyRequestV1 struct {
	Release     ReleaseIdentityV1 `json:"release"`
	PreparedRef string            `json:"preparedRef"`
	RollbackRef string            `json:"rollbackRef"`
}

type VerifyResultV1 struct {
	ProviderRevision string `json:"providerRevision"`
	Verified         bool   `json:"verified"`
	VerifiedSequence uint64 `json:"verifiedSequence"`
	VerifiedDigest   string `json:"verifiedDigest"`
	ManagementReady  bool   `json:"managementReady"`
	HealthRevision   string `json:"healthRevision"`
}

type RollbackRequestV1 struct {
	Release     ReleaseIdentityV1 `json:"release"`
	RollbackRef string            `json:"rollbackRef"`
	ReasonCode  string            `json:"reasonCode"`
}

type RollbackResultV1 struct {
	ProviderRevision string `json:"providerRevision"`
	RolledBack       bool   `json:"rolledBack"`
	ActiveSequence   uint64 `json:"activeSequence"`
	ActiveDigest     string `json:"activeDigest"`
	ManagementReady  bool   `json:"managementReady"`
}
