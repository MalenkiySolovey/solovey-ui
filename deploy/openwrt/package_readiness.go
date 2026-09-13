package openwrt

import (
	"errors"
	"slices"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
)

// PackageGenerationEvidence is the bounded projection needed to decide that
// the package generation opened by procd is the generation currently installed
// on disk. Collection remains with the lifecycle owner; this deployment owner
// defines the OpenWrt-specific relationship between those authenticated facts.
type PackageGenerationEvidence struct {
	Owner          deploymentidentity.ApplicationOwnerContractProcdV1
	Manifest       broker.Manifest
	PanelInstance  broker.ProcdInstanceEvidence
	PanelProcess   processevidence.Fact
	PanelObject    executableobject.Identity
	BrokerInstance broker.ProcdInstanceEvidence
	BrokerProcess  processevidence.Fact
	BrokerObject   executableobject.Identity
}

// ValidatePackageGenerationReadiness accepts only one coherent current
// OpenWrt package generation. It does not inspect the host or perform lifecycle
// operations; callers supply facts collected by their existing bounded owners.
func ValidatePackageGenerationReadiness(evidence PackageGenerationEvidence) error {
	owner := evidence.Owner
	if err := owner.Validate(); err != nil || owner.ProcdService != ProcdServiceName || owner.ProcdInstance != ProcdPanelInstance ||
		owner.ExecutablePath != PanelExecutablePath || owner.ProcessUID == 0 || owner.ProcessGID == 0 {
		return errors.Join(errors.New("current OpenWrt owner contract is invalid"), err)
	}
	finalized, err := broker.FinalizeManifest(evidence.Manifest)
	if err != nil || evidence.Manifest.Schema != broker.ManifestSchemaProcd || finalized.Revision != evidence.Manifest.Revision ||
		evidence.Manifest.ApplicationOwnerRevision != owner.Revision {
		return errors.Join(errors.New("current OpenWrt broker manifest generation is invalid"), err)
	}
	clients := make(map[string]broker.ClientManifest, len(evidence.Manifest.Clients))
	for _, client := range evidence.Manifest.Clients {
		if _, exists := clients[client.Name]; exists {
			return errors.New("current OpenWrt broker manifest client identity is ambiguous")
		}
		clients[client.Name] = client
	}
	panel, panelOK := clients["panel"]
	readiness, readinessOK := clients["broker-readiness"]
	proof, proofOK := clients["ssh-proof"]
	if len(clients) != 3 || !panelOK || !readinessOK || !proofOK ||
		readiness.Executable != ReadinessExecutablePath || readiness.ProcdService != ProcdServiceName || readiness.ProcdInstance != ProcdPanelInstance ||
		proof.Executable != SSHProofExecutablePath || proof.ProcdService != "dropbear" || proof.ProcdRelation != broker.ProcdRelationAncestor ||
		proof.ProcdInstanceSelector != broker.ProcdSelectorUniqueAncestor || !slices.Equal(proof.ProcdCommand, []string{DropbearExecutablePath, "-F"}) {
		return errors.New("current OpenWrt broker manifest client generation is invalid")
	}
	if err := validateInstalledObject(evidence.PanelObject, PanelExecutablePath); err != nil ||
		owner.ExecutableSHA256 != evidence.PanelObject.Digest || owner.ArtifactRevision != "art-"+evidence.PanelObject.Digest ||
		panel.Executable != PanelExecutablePath || panel.ExecutableDigest != evidence.PanelObject.Digest ||
		panel.Device != evidence.PanelObject.Device || panel.Inode != evidence.PanelObject.Inode {
		return errors.Join(errors.New("current OpenWrt panel package generation is invalid"), err)
	}
	if evidence.PanelInstance.Name != ProcdPanelInstance || evidence.PanelInstance.PID != evidence.PanelProcess.PID ||
		!slices.Equal(evidence.PanelInstance.Command, []string{LifecycleExecutablePath, "panel-entry"}) ||
		evidence.PanelInstance.User != PanelAccountName || evidence.PanelInstance.Group != PanelAccountName ||
		!processMatchesObject(evidence.PanelProcess, evidence.PanelObject, PanelExecutablePath) {
		return errors.New("running panel is not the current package generation")
	}
	if err := validateInstalledObject(evidence.BrokerObject, BrokerExecutablePath); err != nil ||
		evidence.BrokerInstance.Name != ProcdBrokerInstance || evidence.BrokerInstance.PID != evidence.BrokerProcess.PID ||
		!slices.Equal(evidence.BrokerInstance.Command, []string{LifecycleExecutablePath, "broker-entry"}) ||
		evidence.BrokerInstance.User != "root" || evidence.BrokerInstance.Group != "root" ||
		!processMatchesObject(evidence.BrokerProcess, evidence.BrokerObject, BrokerExecutablePath) {
		return errors.Join(errors.New("running broker is not the current package generation"), err)
	}
	return nil
}

func validateInstalledObject(identity executableobject.Identity, expectedPath string) error {
	if identity.Label != expectedPath || identity.ResolvedPath == "" || identity.Device == 0 || identity.Inode == 0 ||
		!identity.Mode.IsRegular() || identity.Mode.Perm()&0o022 != 0 || identity.UID != 0 || identity.GID != 0 || !digest64(identity.Digest) {
		return errors.New("current OpenWrt installed executable object is invalid")
	}
	return nil
}

func processMatchesObject(fact processevidence.Fact, object executableobject.Identity, path string) bool {
	return fact.PID > 1 && fact.Executable == path && !fact.ExecutableDeleted && fact.ExeDigest == object.Digest &&
		fact.ExeDevice == object.Device && fact.ExeInode == object.Inode
}
