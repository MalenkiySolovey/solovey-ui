// Package deploymentidentity defines the production-owned identity contract
// used by components that must bind runtime observations to one exact active
// application deployment. It deliberately contains no lab integration.
package deploymentidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strings"
)

const (
	SchemaV1              = "solovey-ui/application-owner-contract/v1"
	InstalledContractPath = "/etc/solovey-ui/application-owner-contract.json"
	MaxContractBytes      = 64 << 10
)

var (
	revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	sourcePattern   = regexp.MustCompile(`^src-[a-f0-9]{64}$`)
	artifactPattern = regexp.MustCompile(`^art-[a-f0-9]{64}$`)
	deployPattern   = regexp.MustCompile(`^dep-[a-f0-9]{64}$`)
	uuidPattern     = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
	unitPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,126}\.service$`)
	identityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:@+-]{0,127}$`)
)

// ApplicationOwnerContractV1 is written by the production deployment owner
// before service activation. Consumers never accept a PID, executable path or
// systemd unit from an API/helper request; they derive those expectations from
// this root-owned contract instead.
type ApplicationOwnerContractV1 struct {
	Schema                      string `json:"schema"`
	Revision                    string `json:"revision"`
	InstanceID                  string `json:"instanceId"`
	SourceRevision              string `json:"sourceRevision"`
	ArtifactRevision            string `json:"artifactRevision"`
	DeploymentID                string `json:"deploymentId"`
	RuntimeRootContractRevision string `json:"runtimeRootContractRevision"`
	RuntimeRootBindingRevision  string `json:"runtimeRootBindingRevision"`
	ServiceIdentity             string `json:"serviceIdentity"`
	SystemdUnit                 string `json:"systemdUnit"`
	ServiceFragmentPath         string `json:"serviceFragmentPath"`
	ServiceUnitSHA256           string `json:"serviceUnitSha256"`
	ServiceControlGroup         string `json:"serviceControlGroup"`
	ExecutablePath              string `json:"executablePath"`
	ExecutableSHA256            string `json:"executableSha256"`
	ProcessUID                  uint32 `json:"processUid"`
	ProcessGID                  uint32 `json:"processGid"`
}

func NewV1(instanceID, sourceRevision, artifactRevision, deploymentID, runtimeContractRevision, runtimeBindingRevision, serviceIdentity, unit, fragmentPath, unitSHA, controlGroup, executablePath, executableSHA string, uid, gid uint32) (ApplicationOwnerContractV1, error) {
	value := ApplicationOwnerContractV1{
		Schema: SchemaV1, InstanceID: instanceID, SourceRevision: sourceRevision,
		ArtifactRevision: artifactRevision, DeploymentID: deploymentID,
		RuntimeRootContractRevision: runtimeContractRevision, RuntimeRootBindingRevision: runtimeBindingRevision,
		ServiceIdentity: serviceIdentity, SystemdUnit: unit, ServiceFragmentPath: fragmentPath,
		ServiceUnitSHA256: unitSHA, ServiceControlGroup: controlGroup,
		ExecutablePath: executablePath, ExecutableSHA256: executableSHA, ProcessUID: uid, ProcessGID: gid,
	}
	revision, err := value.revision()
	if err != nil {
		return ApplicationOwnerContractV1{}, err
	}
	value.Revision = revision
	if err := value.Validate(); err != nil {
		return ApplicationOwnerContractV1{}, err
	}
	return value, nil
}

func (c ApplicationOwnerContractV1) Validate() error {
	if c.Schema != SchemaV1 || !revisionPattern.MatchString(c.Revision) ||
		!uuidPattern.MatchString(c.InstanceID) || !sourcePattern.MatchString(c.SourceRevision) ||
		!artifactPattern.MatchString(c.ArtifactRevision) || !deployPattern.MatchString(c.DeploymentID) ||
		!revisionPattern.MatchString(c.RuntimeRootContractRevision) || !revisionPattern.MatchString(c.RuntimeRootBindingRevision) ||
		!revisionPattern.MatchString(c.ServiceUnitSHA256) || !revisionPattern.MatchString(c.ExecutableSHA256) {
		return errors.New("application owner contract identity is malformed")
	}
	if !identityPattern.MatchString(c.ServiceIdentity) || !unitPattern.MatchString(c.SystemdUnit) ||
		!canonicalAbsolute(c.ServiceFragmentPath) || !canonicalAbsolute(c.ExecutablePath) ||
		!canonicalCgroup(c.ServiceControlGroup) {
		return errors.New("application owner contract service identity is malformed")
	}
	revision, err := c.revision()
	if err != nil || revision != c.Revision {
		return errors.New("application owner contract revision differs")
	}
	return nil
}

func (c ApplicationOwnerContractV1) revision() (string, error) {
	copy := c
	copy.Revision = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalAbsolute(value string) bool {
	return value != "" && strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsRune(value, 0)
}

func canonicalCgroup(value string) bool {
	return canonicalAbsolute(value) && value != "/" && !strings.ContainsAny(value, "\r\n\t")
}
