package deploymentidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const (
	ProcdSchemaV1              = "solovey-ui/application-owner-contract/procd/v1"
	ProcdInstalledContractPath = "/etc/solovey-ui/procd-application-owner-contract.json"
)

// ApplicationOwnerContractProcdV1 is the root-owned deployment projection
// for a named procd service instance. The systemd v1 contract is intentionally
// preserved as a separate schema.
type ApplicationOwnerContractProcdV1 struct {
	Schema                      string `json:"schema"`
	Revision                    string `json:"revision"`
	InstanceID                  string `json:"instanceId"`
	SourceRevision              string `json:"sourceRevision"`
	ArtifactRevision            string `json:"artifactRevision"`
	DeploymentID                string `json:"deploymentId"`
	RuntimeRootContractRevision string `json:"runtimeRootContractRevision"`
	RuntimeRootBindingRevision  string `json:"runtimeRootBindingRevision"`
	ServiceIdentity             string `json:"serviceIdentity"`
	ProcdService                string `json:"procdService"`
	ProcdInstance               string `json:"procdInstance"`
	ExecutablePath              string `json:"executablePath"`
	ExecutableSHA256            string `json:"executableSha256"`
	ProcessUID                  uint32 `json:"processUid"`
	ProcessGID                  uint32 `json:"processGid"`
}

func NewProcdV1(instanceID, sourceRevision, artifactRevision, deploymentID, runtimeContractRevision, runtimeBindingRevision, serviceIdentity, procdService, procdInstance, executablePath, executableSHA string, uid, gid uint32) (ApplicationOwnerContractProcdV1, error) {
	value := ApplicationOwnerContractProcdV1{
		Schema: ProcdSchemaV1, InstanceID: instanceID, SourceRevision: sourceRevision,
		ArtifactRevision: artifactRevision, DeploymentID: deploymentID,
		RuntimeRootContractRevision: runtimeContractRevision, RuntimeRootBindingRevision: runtimeBindingRevision,
		ServiceIdentity: serviceIdentity, ProcdService: procdService, ProcdInstance: procdInstance,
		ExecutablePath: executablePath, ExecutableSHA256: executableSHA, ProcessUID: uid, ProcessGID: gid,
	}
	revision, err := value.revision()
	if err != nil {
		return ApplicationOwnerContractProcdV1{}, err
	}
	value.Revision = revision
	if err := value.Validate(); err != nil {
		return ApplicationOwnerContractProcdV1{}, err
	}
	return value, nil
}

func (c ApplicationOwnerContractProcdV1) Validate() error {
	if c.Schema != ProcdSchemaV1 || !revisionPattern.MatchString(c.Revision) ||
		!uuidPattern.MatchString(c.InstanceID) || !sourcePattern.MatchString(c.SourceRevision) ||
		!artifactPattern.MatchString(c.ArtifactRevision) || !deployPattern.MatchString(c.DeploymentID) ||
		!revisionPattern.MatchString(c.RuntimeRootContractRevision) || !revisionPattern.MatchString(c.RuntimeRootBindingRevision) ||
		!revisionPattern.MatchString(c.ExecutableSHA256) {
		return errors.New("procd application owner contract identity is malformed")
	}
	if !identityPattern.MatchString(c.ServiceIdentity) || !identityPattern.MatchString(c.ProcdService) ||
		!identityPattern.MatchString(c.ProcdInstance) || !canonicalAbsolute(c.ExecutablePath) {
		return errors.New("procd application owner contract service identity is malformed")
	}
	revision, err := c.revision()
	if err != nil || revision != c.Revision {
		return errors.New("procd application owner contract revision differs")
	}
	return nil
}

func (c ApplicationOwnerContractProcdV1) revision() (string, error) {
	copy := c
	copy.Revision = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
