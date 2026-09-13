package runtimecontract

import "github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"

// OpenWrtApplicationOwnerInput holds the fixed, root-owned deployment inputs
// that later procd integration must materialize. It has no supervisor control
// or process discovery authority.
type OpenWrtApplicationOwnerInput struct {
	InstanceID       string
	SourceRevision   string
	ArtifactRevision string
	DeploymentID     string
	ServiceIdentity  string
	ProcdService     string
	ProcdInstance    string
	ExecutablePath   string
	ExecutableSHA256 string
	ProcessUID       uint32
	ProcessGID       uint32
}

func OpenWrtApplicationOwner(input OpenWrtApplicationOwnerInput) (deploymentidentity.ApplicationOwnerContractProcdV1, error) {
	binding, err := BindOpenWrt(OpenWrtInstalled(), input.InstanceID, input.SourceRevision, input.ArtifactRevision, input.DeploymentID)
	if err != nil {
		return deploymentidentity.ApplicationOwnerContractProcdV1{}, err
	}
	return deploymentidentity.NewProcdV1(
		input.InstanceID, input.SourceRevision, input.ArtifactRevision, input.DeploymentID,
		binding.ContractRevision, binding.BindingRevision, input.ServiceIdentity, input.ProcdService, input.ProcdInstance,
		input.ExecutablePath, input.ExecutableSHA256, input.ProcessUID, input.ProcessGID,
	)
}
