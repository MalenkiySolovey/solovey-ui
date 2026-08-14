package runtimecontract

import (
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
)

// ApplicationOwnerInput binds the production runtime contract to the exact
// installed service unit and release executable. The installer and signed
// update broker materialize this contract without external deployment state.
type ApplicationOwnerInput struct {
	InstanceID          string
	SourceRevision      string
	ArtifactRevision    string
	DeploymentID        string
	ServiceIdentity     string
	SystemdUnit         string
	ServiceFragmentPath string
	ServiceUnitSHA256   string
	ServiceControlGroup string
	ExecutablePath      string
	ExecutableSHA256    string
	ProcessUID          uint32
	ProcessGID          uint32
}

func ApplicationOwner(input ApplicationOwnerInput) (deploymentidentity.ApplicationOwnerContractV1, error) {
	binding, err := Bind(Installed(), input.InstanceID, input.SourceRevision, input.ArtifactRevision, input.DeploymentID)
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractV1{}, err
	}
	if strings.TrimSpace(input.ServiceIdentity) == "" || strings.TrimSpace(input.SystemdUnit) == "" {
		return deploymentidentity.ApplicationOwnerContractV1{}, errors.New("application owner service identity is missing")
	}
	return deploymentidentity.NewV1(
		input.InstanceID, input.SourceRevision, input.ArtifactRevision, input.DeploymentID,
		binding.ContractRevision, binding.BindingRevision, input.ServiceIdentity, input.SystemdUnit,
		input.ServiceFragmentPath, input.ServiceUnitSHA256, input.ServiceControlGroup,
		input.ExecutablePath, input.ExecutableSHA256, input.ProcessUID, input.ProcessGID,
	)
}
