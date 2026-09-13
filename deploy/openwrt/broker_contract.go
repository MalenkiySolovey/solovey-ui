package openwrt

import (
	"errors"
	"path"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

const (
	ProcdServiceName        = "solovey-ui"
	ProcdBrokerInstance     = "root-broker"
	ProcdPanelInstance      = "panel"
	PanelAccountName        = "solovey-ui"
	BrokerExecutablePath    = DefaultInstallRoot + "/solovey-privileged-broker"
	ReadinessExecutablePath = DefaultInstallRoot + "/solovey-broker-readiness"
	LifecycleExecutablePath = DefaultInstallRoot + "/solovey-openwrt-lifecycle"
	PanelExecutablePath     = DefaultInstallRoot + "/solovey-ui"
	SSHProofExecutablePath  = DefaultInstallRoot + "/solovey-ssh-proof"
	DropbearExecutablePath  = "/usr/sbin/dropbear"
)

// ClientExecutableIdentity is materialized by the package owner after the
// exact root-owned binaries are installed. It contains no discovery API.
type ClientExecutableIdentity struct {
	Path   string
	SHA256 string
	Device uint64
	Inode  uint64
}

// ProcdBrokerManifest projects the fixed panel instance into the existing
// broker manifest model. The readiness helper is the procd main process before
// exec and the panel retains that PID afterward, so both exact executables are
// authorized while service/instance evidence remains bound to one process.
func ProcdBrokerManifest(owner deploymentidentity.ApplicationOwnerContractProcdV1, panel, readiness, proof, dropbear ClientExecutableIdentity) (broker.Manifest, error) {
	if err := owner.Validate(); err != nil || owner.ProcdService != ProcdServiceName || owner.ProcdInstance != ProcdPanelInstance ||
		owner.ExecutablePath != panel.Path || owner.ExecutableSHA256 != panel.SHA256 || owner.ProcessUID == 0 || owner.ProcessGID == 0 {
		return broker.Manifest{}, errors.New("OpenWrt broker application owner identity is invalid")
	}
	if panel.Path != PanelExecutablePath || readiness.Path != ReadinessExecutablePath || proof.Path != SSHProofExecutablePath || dropbear.Path != DropbearExecutablePath ||
		!digest64(panel.SHA256) || !digest64(readiness.SHA256) || panel.Device == 0 || panel.Inode == 0 ||
		readiness.Device == 0 || readiness.Inode == 0 || !digest64(proof.SHA256) || proof.Device == 0 || proof.Inode == 0 ||
		!digest64(dropbear.SHA256) || dropbear.Device == 0 || dropbear.Inode == 0 {
		return broker.Manifest{}, errors.New("OpenWrt broker client executable identity is invalid")
	}
	command := []string{LifecycleExecutablePath, "panel-entry"}
	client := func(name string, executable ClientExecutableIdentity, capabilitiesOnly bool) broker.ClientManifest {
		return broker.ClientManifest{
			Name: name, UID: owner.ProcessUID, GID: owner.ProcessGID, Executable: executable.Path, ExecutableDigest: executable.SHA256,
			Device: executable.Device, Inode: executable.Inode, Roles: []broker.Role{broker.RolePanel},
			CgroupPolicy: broker.CgroupOptional, CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1,
			ProcdService: owner.ProcdService, ProcdInstance: owner.ProcdInstance, ProcdCommand: command,
			ProcdUser: PanelAccountName, ProcdGroup: PanelAccountName, ProcdRelation: broker.ProcdRelationMain,
			CapabilitiesOnly: capabilitiesOnly,
		}
	}
	proofClient := broker.ClientManifest{Name: "ssh-proof", AnyNonRootUID: true, AnyGID: true, RequiredGroup: owner.ProcessGID,
		Executable: proof.Path, ExecutableDigest: proof.SHA256, Device: proof.Device, Inode: proof.Inode, Roles: []broker.Role{broker.RoleSSHProof},
		CgroupPolicy: broker.CgroupOptional, CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1,
		ProcdService: "dropbear", ProcdInstanceSelector: broker.ProcdSelectorUniqueAncestor, ProcdCommand: []string{DropbearExecutablePath, "-F"},
		ProcdUser: "root", ProcdGroup: "root", ProcdRelation: broker.ProcdRelationAncestor,
		ProcdExecutable: dropbear.Path, ProcdExecutableDigest: dropbear.SHA256, ProcdDevice: dropbear.Device, ProcdInode: dropbear.Inode}
	return broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaProcd, ApplicationOwnerRevision: owner.Revision, Clients: []broker.ClientManifest{
		client("panel", panel, false), client("broker-readiness", readiness, true), proofClient,
	}})
}

func BrokerSocketPaths(profile ProfileV1) (string, string, error) {
	if err := profile.Validate(); err != nil || profile.BrokerSocketRoot != broker.StandaloneSocketRoot {
		return "", "", errors.New("OpenWrt broker socket profile is unsupported")
	}
	return path.Join(profile.BrokerSocketRoot, path.Base(broker.DefaultSocketPath)),
		path.Join(profile.BrokerSocketRoot, path.Base(broker.ProofSocketPath)), nil
}

func digest64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return false
		}
	}
	return true
}
