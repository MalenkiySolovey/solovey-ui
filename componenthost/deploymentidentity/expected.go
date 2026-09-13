package deploymentidentity

import "errors"

const ExpectedApplicationOwnerSchemaV1 = "solovey-ui/expected-application-owner/v1"

type InstalledApplicationBackend string

const (
	InstalledApplicationBackendSystemd InstalledApplicationBackend = "systemd"
	InstalledApplicationBackendProcd   InstalledApplicationBackend = "procd"
)

// ExpectedApplicationOwnerV1 is the supervisor-neutral proposition consumed by
// resource inventory and components. ContractRevision is the reference to the
// separately serialized, deployment-owned backend proof that produced it.
type ExpectedApplicationOwnerV1 struct {
	Schema                     string `json:"schema"`
	ContractRevision           string `json:"contractRevision"`
	InstanceID                 string `json:"instanceId"`
	SourceRevision             string `json:"sourceRevision"`
	ArtifactRevision           string `json:"artifactRevision"`
	DeploymentID               string `json:"deploymentId"`
	RuntimeRootBindingRevision string `json:"runtimeRootBindingRevision"`
	ServiceIdentity            string `json:"serviceIdentity"`
	ExecutablePath             string `json:"executablePath"`
	ExecutableSHA256           string `json:"executableSha256"`
	ProcessUID                 uint32 `json:"processUid"`
	ProcessGID                 uint32 `json:"processGid"`
}

func (e ExpectedApplicationOwnerV1) Validate() error {
	if e.Schema != ExpectedApplicationOwnerSchemaV1 || !revisionPattern.MatchString(e.ContractRevision) ||
		!uuidPattern.MatchString(e.InstanceID) || !sourcePattern.MatchString(e.SourceRevision) ||
		!artifactPattern.MatchString(e.ArtifactRevision) || !deployPattern.MatchString(e.DeploymentID) ||
		!revisionPattern.MatchString(e.RuntimeRootBindingRevision) || !identityPattern.MatchString(e.ServiceIdentity) ||
		!canonicalAbsolute(e.ExecutablePath) || !revisionPattern.MatchString(e.ExecutableSHA256) {
		return errors.New("expected application owner is malformed")
	}
	return nil
}

func (e ExpectedApplicationOwnerV1) Valid() bool { return e.Validate() == nil }

// InstalledApplicationOwnerProjection retains the exact backend proof selected
// for an installed application owner. Runtime-root and recovery consumers use
// this one value instead of independently rediscovering the installed backend.
type InstalledApplicationOwnerProjection struct {
	Backend InstalledApplicationBackend
	Owner   ExpectedApplicationOwnerV1
}

func NewInstalledApplicationOwnerProjection(backend InstalledApplicationBackend, owner ExpectedApplicationOwnerV1) (InstalledApplicationOwnerProjection, error) {
	projection := InstalledApplicationOwnerProjection{Backend: backend, Owner: owner}
	return projection, projection.Validate()
}

func (p InstalledApplicationOwnerProjection) Validate() error {
	if p.Backend != InstalledApplicationBackendSystemd && p.Backend != InstalledApplicationBackendProcd {
		return errors.New("installed application owner backend is invalid")
	}
	return p.Owner.Validate()
}

// ExpectedSystemdApplicationOwner adapts the released Systemd-specific owner
// contract into the supervisor-neutral application-owner fact without
// changing the released serialization.
func ExpectedSystemdApplicationOwner(contract ApplicationOwnerContractV1) (ExpectedApplicationOwnerV1, error) {
	if err := contract.Validate(); err != nil {
		return ExpectedApplicationOwnerV1{}, err
	}
	return expectedApplicationOwner(
		contract.Revision, contract.InstanceID, contract.SourceRevision, contract.ArtifactRevision,
		contract.DeploymentID, contract.RuntimeRootBindingRevision, contract.ServiceIdentity,
		contract.ExecutablePath, contract.ExecutableSHA256, contract.ProcessUID, contract.ProcessGID,
	)
}

// ExpectedProcdApplicationOwner projects the procd installed contract into
// the same semantic proposition as the Systemd adapter.
func ExpectedProcdApplicationOwner(contract ApplicationOwnerContractProcdV1) (ExpectedApplicationOwnerV1, error) {
	if err := contract.Validate(); err != nil {
		return ExpectedApplicationOwnerV1{}, err
	}
	return expectedApplicationOwner(
		contract.Revision, contract.InstanceID, contract.SourceRevision, contract.ArtifactRevision,
		contract.DeploymentID, contract.RuntimeRootBindingRevision, contract.ServiceIdentity,
		contract.ExecutablePath, contract.ExecutableSHA256, contract.ProcessUID, contract.ProcessGID,
	)
}

func expectedApplicationOwner(contractRevision, instanceID, sourceRevision, artifactRevision, deploymentID,
	runtimeBindingRevision, serviceIdentity, executablePath, executableSHA string, uid, gid uint32) (ExpectedApplicationOwnerV1, error) {
	value := ExpectedApplicationOwnerV1{
		Schema: ExpectedApplicationOwnerSchemaV1, ContractRevision: contractRevision,
		InstanceID: instanceID, SourceRevision: sourceRevision, ArtifactRevision: artifactRevision,
		DeploymentID: deploymentID, RuntimeRootBindingRevision: runtimeBindingRevision,
		ServiceIdentity: serviceIdentity, ExecutablePath: executablePath, ExecutableSHA256: executableSHA,
		ProcessUID: uid, ProcessGID: gid,
	}
	return value, value.Validate()
}
