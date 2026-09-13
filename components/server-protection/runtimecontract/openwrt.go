package runtimecontract

import (
	"encoding/json"
	"errors"
	"path"
)

const (
	OpenWrtRuntimeSchemaV1        = "solovey-ui/server-protection-runtime/openwrt/v1"
	OpenWrtRuntimeBindingSchemaV1 = "solovey-ui/server-protection-runtime-binding/openwrt/v1"
	OpenWrtRuntimeRoot            = "/run/solovey-ui/server-protection"
)

// OpenWrtRuntimeRootContractV1 is the deployment-owned runtime layout for
// OpenWrt. It is separate from durable database state and the systemd
// RuntimeRootContract.
type OpenWrtRuntimeRootContractV1 struct {
	Schema            string `json:"schema"`
	ComponentID       string `json:"componentId"`
	RuntimeRoot       string `json:"runtimeRoot"`
	ArtifactRoot      string `json:"artifactRoot"`
	RevisionRoot      string `json:"revisionRoot"`
	OperationsRoot    string `json:"operationsRoot"`
	RecoveryRoot      string `json:"recoveryRoot"`
	HelperManagedRoot string `json:"helperManagedRoot"`
	OwnerIdentity     string `json:"ownerIdentity"`
	MutationAuthority string `json:"mutationAuthority"`
	DirectoryMode     uint32 `json:"directoryMode"`
	SymlinkPolicy     string `json:"symlinkPolicy"`
	MountPolicy       string `json:"mountPolicy"`
}

// OpenWrtBindingV1 binds an OpenWrt runtime layout to one exact deployment.
type OpenWrtBindingV1 struct {
	Schema           string                       `json:"schema"`
	Contract         OpenWrtRuntimeRootContractV1 `json:"contract"`
	ContractRevision string                       `json:"contractRevision"`
	InstanceID       string                       `json:"instanceId"`
	SourceRevision   string                       `json:"sourceRevision"`
	ArtifactRevision string                       `json:"artifactRevision"`
	DeploymentID     string                       `json:"deploymentId"`
	BindingRevision  string                       `json:"bindingRevision"`
}

// OpenWrtInstalled returns the fixed initial OpenWrt runtime policy. The
// deployment profile that injects database state does not select it at runtime.
func OpenWrtInstalled() OpenWrtRuntimeRootContractV1 {
	runtimeRoot := OpenWrtRuntimeRoot
	return OpenWrtRuntimeRootContractV1{
		Schema:            OpenWrtRuntimeSchemaV1,
		ComponentID:       "server-protection",
		RuntimeRoot:       runtimeRoot,
		ArtifactRoot:      runtimeRoot,
		RevisionRoot:      path.Join(runtimeRoot, "revisions"),
		OperationsRoot:    path.Join(runtimeRoot, "operations"),
		RecoveryRoot:      path.Join(runtimeRoot, "recovery"),
		HelperManagedRoot: runtimeRoot,
		OwnerIdentity:     "solovey-ui-service-account",
		MutationAuthority: "solovey-privileged-broker",
		DirectoryMode:     0o700,
		SymlinkPolicy:     "reject",
		MountPolicy:       "require-volatile-runtime-mount",
	}
}

func (c OpenWrtRuntimeRootContractV1) Validate() error {
	if c.Schema != OpenWrtRuntimeSchemaV1 || c.ComponentID != "server-protection" ||
		!canonicalLinuxAbsolute(c.RuntimeRoot) || c.RuntimeRoot != OpenWrtRuntimeRoot {
		return errors.New("OpenWrt runtime root contract schema or root is unsupported")
	}
	if c.ArtifactRoot != c.RuntimeRoot || c.HelperManagedRoot != c.RuntimeRoot ||
		c.RevisionRoot != path.Join(c.RuntimeRoot, "revisions") ||
		c.OperationsRoot != path.Join(c.RuntimeRoot, "operations") ||
		c.RecoveryRoot != path.Join(c.RuntimeRoot, "recovery") {
		return errors.New("OpenWrt runtime root contract subroots are inconsistent")
	}
	if c.OwnerIdentity != "solovey-ui-service-account" || c.MutationAuthority != "solovey-privileged-broker" ||
		c.DirectoryMode != 0o700 || c.SymlinkPolicy != "reject" || c.MountPolicy != "require-volatile-runtime-mount" {
		return errors.New("OpenWrt runtime root contract ownership or mount policy is unsafe")
	}
	return nil
}

func (c OpenWrtRuntimeRootContractV1) Revision() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func BindOpenWrt(contract OpenWrtRuntimeRootContractV1, instanceID, sourceRevision, artifactRevision, deploymentID string) (OpenWrtBindingV1, error) {
	contractRevision, err := contract.Revision()
	if err != nil {
		return OpenWrtBindingV1{}, err
	}
	if !uuidPattern.MatchString(instanceID) || !sourcePattern.MatchString(sourceRevision) ||
		!artifactPattern.MatchString(artifactRevision) || !deployPattern.MatchString(deploymentID) {
		return OpenWrtBindingV1{}, errors.New("OpenWrt runtime root binding identity is malformed")
	}
	binding := OpenWrtBindingV1{
		Schema: OpenWrtRuntimeBindingSchemaV1, Contract: contract, ContractRevision: contractRevision,
		InstanceID: instanceID, SourceRevision: sourceRevision, ArtifactRevision: artifactRevision, DeploymentID: deploymentID,
	}
	revision, err := binding.revision()
	if err != nil {
		return OpenWrtBindingV1{}, err
	}
	binding.BindingRevision = revision
	return binding, nil
}

func (b OpenWrtBindingV1) Validate() error {
	if b.Schema != OpenWrtRuntimeBindingSchemaV1 || !revisionPattern.MatchString(b.ContractRevision) ||
		!revisionPattern.MatchString(b.BindingRevision) {
		return errors.New("OpenWrt runtime root binding schema or revision is malformed")
	}
	contractRevision, err := b.Contract.Revision()
	if err != nil || contractRevision != b.ContractRevision {
		return errors.New("OpenWrt runtime root binding contract revision differs")
	}
	if !uuidPattern.MatchString(b.InstanceID) || !sourcePattern.MatchString(b.SourceRevision) ||
		!artifactPattern.MatchString(b.ArtifactRevision) || !deployPattern.MatchString(b.DeploymentID) {
		return errors.New("OpenWrt runtime root binding identity is malformed")
	}
	revision, err := b.revision()
	if err != nil || revision != b.BindingRevision {
		return errors.New("OpenWrt runtime root binding revision differs")
	}
	return nil
}

func (b OpenWrtBindingV1) revision() (string, error) {
	copy := b
	copy.BindingRevision = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}
