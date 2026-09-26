package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/deploy/openwrt/persistencepolicy"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

const (
	InstalledRuntimeRootSchemaV1 = "solovey-ui/server-protection-installed-runtime-root/v1"
	InstalledRuntimeRootPath     = "/etc/solovey-ui/server-protection-runtime-root.json"
	RuntimeMountProofSchemaV1    = "solovey-ui/server-protection-runtime-mount/v1"
	RuntimeMountVolatile         = "volatile"
	RuntimeMountPersistent       = "persistent"
	DockerRuntimeRoot            = "/run/solovey-ui/server-protection"
)

type DeploymentBackend string

const (
	DeploymentBackendSystemd DeploymentBackend = "systemd"
	DeploymentBackendProcd   DeploymentBackend = "procd"
	DeploymentBackendDocker  DeploymentBackend = "docker"
)

type RuntimeMountProofV1 struct {
	Schema   string             `json:"schema"`
	Revision string             `json:"revision"`
	Policy   string             `json:"policy"`
	Root     string             `json:"root"`
	Mount    mountevidence.Fact `json:"mount"`
}

// InstalledRuntimeRootV1 is the single deployment-written location fact
// consumed by Server Protection. Backend-specific runtime policy contracts
// remain separate; this projection binds their selected root to the same exact
// deployment and owner proof used for listener ownership.
type InstalledRuntimeRootV1 struct {
	Schema                      string              `json:"schema"`
	Revision                    string              `json:"revision"`
	ComponentID                 string              `json:"componentId"`
	RuntimeRoot                 string              `json:"runtimeRoot"`
	RuntimeRootContractRevision string              `json:"runtimeRootContractRevision"`
	RuntimeRootBindingRevision  string              `json:"runtimeRootBindingRevision"`
	OwnerContractRevision       string              `json:"ownerContractRevision"`
	InstanceID                  string              `json:"instanceId"`
	SourceRevision              string              `json:"sourceRevision"`
	ArtifactRevision            string              `json:"artifactRevision"`
	DeploymentID                string              `json:"deploymentId"`
	DirectoryMode               uint32              `json:"directoryMode"`
	MountProof                  RuntimeMountProofV1 `json:"mountProof"`
}

// RuntimeRootAuthority is the opaque semantic root selected from either an
// authenticated installed deployment binding or the existing development
// database layout. Consumers can inspect the selected path but cannot forge a
// different authority by constructing this value themselves.
type RuntimeRootAuthority struct {
	root                  string
	installed             bool
	mount                 RuntimeMountProofV1
	backend               DeploymentBackend
	installedRoot         InstalledRuntimeRootV1
	ownerProjection       deploymentidentity.InstalledApplicationOwnerProjection
	dockerProfileID       string
	dockerProfileRevision string
}

func (a RuntimeRootAuthority) Path() string { return a.root }
func (a RuntimeRootAuthority) CanonicalPath() string {
	if a.installed {
		return a.mount.Mount.ResolvedTarget
	}
	return a.root
}
func (a RuntimeRootAuthority) Installed() bool            { return a.installed }
func (a RuntimeRootAuthority) Backend() DeploymentBackend { return a.backend }
func (a RuntimeRootAuthority) OwnerContractRevision() string {
	return a.ownerProjection.Owner.ContractRevision
}

// ProjectionRevision identifies the complete retained runtime-root/backend
// generation used by downstream storage and recovery consumers.
func (a RuntimeRootAuthority) ProjectionRevision() string {
	data, _ := json.Marshal(struct {
		Root                  string            `json:"root"`
		CanonicalRoot         string            `json:"canonicalRoot"`
		Backend               DeploymentBackend `json:"backend"`
		RuntimeRevision       string            `json:"runtimeRevision,omitempty"`
		OwnerRevision         string            `json:"ownerRevision,omitempty"`
		MountRevision         string            `json:"mountRevision"`
		DockerProfile         string            `json:"dockerProfile,omitempty"`
		DockerProfileRevision string            `json:"dockerProfileRevision,omitempty"`
	}{a.root, a.CanonicalPath(), a.backend, a.installedRoot.Revision, a.ownerProjection.Owner.ContractRevision, a.mount.Revision, a.dockerProfileID, a.dockerProfileRevision})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a RuntimeRootAuthority) Validate() error {
	if a.installed {
		if !canonicalLinuxAbsolute(a.root) || a.root == "/" || a.mount.Validate() != nil || a.mount.Root != a.root ||
			!canonicalLinuxAbsolute(a.CanonicalPath()) || a.CanonicalPath() == "/" {
			return errors.New("installed Server Protection runtime root authority is malformed")
		}
		switch a.backend {
		case DeploymentBackendSystemd, DeploymentBackendProcd:
			if a.installedRoot.RuntimeRoot != a.root || a.installedRoot.MountProof.Revision != a.mount.Revision ||
				a.ownerProjection.Validate() != nil || ValidateInstalledRuntimeRootBinding(a.installedRoot, a.ownerProjection.Owner) != nil {
				return errors.New("installed Server Protection deployment projection is malformed")
			}
			if a.backend == DeploymentBackendSystemd && a.ownerProjection.Backend != deploymentidentity.InstalledApplicationBackendSystemd ||
				a.backend == DeploymentBackendProcd && a.ownerProjection.Backend != deploymentidentity.InstalledApplicationBackendProcd {
				return errors.New("installed Server Protection deployment backend differs")
			}
		case DeploymentBackendDocker:
			if a.mount.Policy != RuntimeMountVolatile || a.root != DockerRuntimeRoot ||
				(a.dockerProfileID == "") != (a.dockerProfileRevision == "") ||
				a.dockerProfileRevision != "" && !revisionPattern.MatchString(a.dockerProfileRevision) {
				return errors.New("docker server protection deployment projection is malformed")
			}
		default:
			return errors.New("installed Server Protection deployment backend is unavailable")
		}
		return nil
	}
	if a.backend != "" || a.installedRoot.Schema != "" || a.ownerProjection.Backend != "" || a.dockerProfileID != "" || a.dockerProfileRevision != "" {
		return errors.New("development Server Protection runtime root has installed deployment facts")
	}
	if canonicalLinuxAbsolute(filepath.ToSlash(a.root)) && filepath.ToSlash(a.root) != "/" {
		return nil
	}
	if a.root == "" || !filepath.IsAbs(a.root) || filepath.Clean(a.root) != a.root {
		return errors.New("server protection runtime root authority is malformed")
	}
	return nil
}

func (a RuntimeRootAuthority) Recheck() error {
	if err := a.Validate(); err != nil || !a.installed {
		return err
	}
	return recheckRuntimeRootAuthority(a)
}

func recheckRetainedInstalledProjection(authority RuntimeRootAuthority,
	loadProjection func() (deploymentidentity.InstalledApplicationOwnerProjection, error),
	loadRuntime func() (InstalledRuntimeRootV1, error), recheckMount func() error) error {
	if err := authority.Validate(); err != nil || authority.backend != DeploymentBackendSystemd && authority.backend != DeploymentBackendProcd {
		return errors.New("installed Server Protection retained projection is invalid")
	}
	currentProjection, err := loadProjection()
	if err != nil {
		return err
	}
	if currentProjection != authority.ownerProjection {
		return errors.New("installed application owner projection changed")
	}
	currentRuntime, err := loadRuntime()
	if err != nil {
		return err
	}
	if currentRuntime.Revision != authority.installedRoot.Revision {
		return errors.New("installed Server Protection runtime root projection changed")
	}
	if recheckMount == nil {
		return errors.New("installed Server Protection mount recheck is unavailable")
	}
	return recheckMount()
}

func NewRuntimeMountProof(root, policy string, fact mountevidence.Fact) (RuntimeMountProofV1, error) {
	proof := RuntimeMountProofV1{Schema: RuntimeMountProofSchemaV1, Policy: policy, Root: root, Mount: fact}
	proof.Revision = proof.revision()
	return proof, proof.Validate()
}

func (proof RuntimeMountProofV1) Validate() error {
	if proof.Schema != RuntimeMountProofSchemaV1 || !canonicalLinuxAbsolute(proof.Root) || proof.Root == "/" ||
		proof.Mount.Validate() != nil || proof.Mount.Target != proof.Root ||
		!sameOwnerLocalRoot(proof.Root, proof.Mount.ResolvedTarget) || !proof.Mount.Writable() || proof.Revision != proof.revision() {
		return errors.New("server protection runtime mount proof is malformed")
	}
	filesystem := strings.ToLower(proof.Mount.Filesystem)
	switch proof.Policy {
	case RuntimeMountVolatile:
		if filesystem != "tmpfs" && filesystem != "ramfs" {
			return errors.New("server protection volatile runtime root is not on a volatile filesystem")
		}
	case RuntimeMountPersistent:
		if !persistencepolicy.ApprovedPersistentFilesystem(filesystem) {
			return errors.New("server protection persistent runtime root is not on an approved filesystem")
		}
	default:
		return errors.New("server protection runtime mount policy is unsupported")
	}
	return nil
}

// The deployment may expose ancestors through filesystem symlinks, but the
// semantic owner's namespace parent and component directory must remain the
// same two path identities after resolution. Owner-local adapters separately
// require those two live objects to be non-symlink directories.
func sameOwnerLocalRoot(logical, resolved string) bool {
	return path.Base(logical) == path.Base(resolved) &&
		path.Base(path.Dir(logical)) == path.Base(path.Dir(resolved))
}

func (proof RuntimeMountProofV1) revision() string {
	copy := proof
	copy.Revision = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// InstalledRuntimeRootAuthority binds the installed location fact to the same
// semantic application-owner projection consumed by the panel and broker.
func InstalledRuntimeRootAuthority(runtime InstalledRuntimeRootV1, projection deploymentidentity.InstalledApplicationOwnerProjection) (RuntimeRootAuthority, error) {
	if err := projection.Validate(); err != nil {
		return RuntimeRootAuthority{}, err
	}
	if err := ValidateInstalledRuntimeRootBinding(runtime, projection.Owner); err != nil {
		return RuntimeRootAuthority{}, err
	}
	backend := DeploymentBackendSystemd
	if projection.Backend == deploymentidentity.InstalledApplicationBackendProcd {
		backend = DeploymentBackendProcd
	}
	authority := RuntimeRootAuthority{root: runtime.RuntimeRoot, installed: true, mount: runtime.MountProof,
		backend: backend, installedRoot: runtime, ownerProjection: projection}
	return authority, authority.Validate()
}

func DockerRuntimeRootAuthority(mount RuntimeMountProofV1) (RuntimeRootAuthority, error) {
	return dockerRuntimeRootAuthority(mount, "", "")
}

func dockerRuntimeRootAuthority(mount RuntimeMountProofV1, profileID, profileRevision string) (RuntimeRootAuthority, error) {
	if mount.Policy != RuntimeMountVolatile || mount.Root != DockerRuntimeRoot || mount.Validate() != nil {
		return RuntimeRootAuthority{}, errors.New("docker server protection runtime root proof is invalid")
	}
	authority := RuntimeRootAuthority{root: DockerRuntimeRoot, installed: true, mount: mount,
		backend: DeploymentBackendDocker, dockerProfileID: profileID, dockerProfileRevision: profileRevision}
	return authority, authority.Validate()
}

// RootAuthorityForDatabaseFolder retains the existing development/custom
// layout without allowing callers to substitute a root directly.
func RootAuthorityForDatabaseFolder(databaseFolder string) (RuntimeRootAuthority, error) {
	root := RootForDatabaseFolder(databaseFolder)
	linuxRoot := filepath.ToSlash(root)
	if canonicalLinuxAbsolute(linuxRoot) && linuxRoot != "/" {
		authority := RuntimeRootAuthority{root: linuxRoot}
		return authority, authority.Validate()
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return RuntimeRootAuthority{}, err
	}
	authority := RuntimeRootAuthority{root: filepath.Clean(root)}
	return authority, authority.Validate()
}

// InstalledSystemdRuntimeRoot projects the released Systemd owner contract
// into the supervisor-neutral installed runtime-root fact.
func InstalledSystemdRuntimeRoot(contract deploymentidentity.ApplicationOwnerContractV1, mount RuntimeMountProofV1) (InstalledRuntimeRootV1, error) {
	runtimePolicy := Installed()
	revision, err := runtimePolicy.Revision()
	if err != nil || contract.Validate() != nil || contract.RuntimeRootContractRevision != revision {
		return InstalledRuntimeRootV1{}, errors.New("systemd runtime root proof does not match its installed owner contract")
	}
	return newInstalledRuntimeRoot(runtimePolicy.RuntimeRoot, revision, contract.RuntimeRootBindingRevision, contract.Revision,
		contract.InstanceID, contract.SourceRevision, contract.ArtifactRevision, contract.DeploymentID, runtimePolicy.DirectoryMode, mount)
}

func InstalledOpenWrtRuntimeRoot(contract deploymentidentity.ApplicationOwnerContractProcdV1, mount RuntimeMountProofV1) (InstalledRuntimeRootV1, error) {
	runtimePolicy := OpenWrtInstalled()
	revision, err := runtimePolicy.Revision()
	if err != nil || contract.Validate() != nil || contract.RuntimeRootContractRevision != revision {
		return InstalledRuntimeRootV1{}, errors.New("OpenWrt runtime root proof does not match its installed owner contract")
	}
	return newInstalledRuntimeRoot(runtimePolicy.RuntimeRoot, revision, contract.RuntimeRootBindingRevision, contract.Revision,
		contract.InstanceID, contract.SourceRevision, contract.ArtifactRevision, contract.DeploymentID, runtimePolicy.DirectoryMode, mount)
}

func newInstalledRuntimeRoot(root, contractRevision, bindingRevision, ownerRevision, instanceID, sourceRevision,
	artifactRevision, deploymentID string, mode uint32, mount RuntimeMountProofV1) (InstalledRuntimeRootV1, error) {
	value := InstalledRuntimeRootV1{
		Schema: InstalledRuntimeRootSchemaV1, ComponentID: "server-protection", RuntimeRoot: root,
		RuntimeRootContractRevision: contractRevision, RuntimeRootBindingRevision: bindingRevision,
		OwnerContractRevision: ownerRevision, InstanceID: instanceID, SourceRevision: sourceRevision,
		ArtifactRevision: artifactRevision, DeploymentID: deploymentID, DirectoryMode: mode, MountProof: mount,
	}
	value.Revision = value.revision()
	return value, value.Validate()
}

func (c InstalledRuntimeRootV1) Validate() error {
	if c.Schema != InstalledRuntimeRootSchemaV1 || c.ComponentID != "server-protection" || !canonicalLinuxAbsolute(c.RuntimeRoot) || c.RuntimeRoot == "/" ||
		!revisionPattern.MatchString(c.Revision) || !revisionPattern.MatchString(c.RuntimeRootContractRevision) ||
		!revisionPattern.MatchString(c.RuntimeRootBindingRevision) || !revisionPattern.MatchString(c.OwnerContractRevision) ||
		!uuidPattern.MatchString(c.InstanceID) || !sourcePattern.MatchString(c.SourceRevision) ||
		!artifactPattern.MatchString(c.ArtifactRevision) || !deployPattern.MatchString(c.DeploymentID) || c.DirectoryMode != 0o700 ||
		c.MountProof.Validate() != nil || c.MountProof.Root != c.RuntimeRoot ||
		c.Revision != c.revision() {
		return errors.New("installed Server Protection runtime root is malformed")
	}
	return nil
}

func (c InstalledRuntimeRootV1) revision() string {
	copy := c
	copy.Revision = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ValidateInstalledRuntimeRootBinding(runtime InstalledRuntimeRootV1, owner deploymentidentity.ExpectedApplicationOwnerV1) error {
	if err := runtime.Validate(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil || runtime.OwnerContractRevision != owner.ContractRevision ||
		runtime.RuntimeRootBindingRevision != owner.RuntimeRootBindingRevision || runtime.InstanceID != owner.InstanceID ||
		runtime.SourceRevision != owner.SourceRevision || runtime.ArtifactRevision != owner.ArtifactRevision ||
		runtime.DeploymentID != owner.DeploymentID {
		return errors.New("installed Server Protection runtime root is bound to a different deployment")
	}
	return nil
}

func resolveRootAuthority(databaseFolder string, loadRuntime func() (InstalledRuntimeRootV1, error),
	loadProjection func() (deploymentidentity.InstalledApplicationOwnerProjection, error)) (RuntimeRootAuthority, error) {
	installed, err := loadRuntime()
	if err == nil {
		projection, projectionErr := loadProjection()
		if projectionErr != nil {
			return RuntimeRootAuthority{}, projectionErr
		}
		return InstalledRuntimeRootAuthority(installed, projection)
	}
	if !errors.Is(err, ErrInstalledRuntimeRootUnavailable) {
		return RuntimeRootAuthority{}, err
	}
	if _, ownerErr := loadProjection(); ownerErr == nil {
		return RuntimeRootAuthority{}, errors.New("installed application owner has no Server Protection runtime root binding")
	} else if !errors.Is(ownerErr, deploymentidentity.ErrExpectedApplicationOwnerUnavailable) {
		return RuntimeRootAuthority{}, ownerErr
	}
	return RootAuthorityForDatabaseFolder(databaseFolder)
}
