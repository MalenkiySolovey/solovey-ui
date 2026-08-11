// Package runtimecontract defines the versioned filesystem and lifecycle
// contract for server-protection runtime state. It contains no lab or host
// integration and is safe for deployment verifiers to import.
package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	SchemaVersion    = 2
	BindingVersion   = 1
	MigrationVersion = 1

	InstalledRoot  = "/usr/local/solovey-ui"
	NativeDataRoot = "/var/lib/solovey-ui"
	DeprecatedRoot = "/var/lib/solovey-ui/.runtime/server-protection"
)

var (
	revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	sourcePattern   = regexp.MustCompile(`^src-[a-f0-9]{64}$`)
	artifactPattern = regexp.MustCompile(`^art-[a-f0-9]{64}$`)
	deployPattern   = regexp.MustCompile(`^dep-[a-f0-9]{64}$`)
	uuidPattern     = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
)

// RuntimeRootContract is the single production contract for the component's
// mutable rollback/recovery state. The panel service owns declarative
// artifacts; only the separately attested broker may perform host mutation.
type RuntimeRootContract struct {
	SchemaVersion     int      `json:"schemaVersion"`
	ComponentID       string   `json:"componentId"`
	RuntimeRoot       string   `json:"runtimeRoot"`
	ArtifactRoot      string   `json:"artifactRoot"`
	RevisionRoot      string   `json:"revisionRoot"`
	OperationsRoot    string   `json:"operationsRoot"`
	RecoveryRoot      string   `json:"recoveryRoot"`
	HelperManagedRoot string   `json:"helperManagedRoot"`
	OwnerIdentity     string   `json:"ownerIdentity"`
	MutationAuthority string   `json:"mutationAuthority"`
	DirectoryMode     uint32   `json:"directoryMode"`
	SymlinkPolicy     string   `json:"symlinkPolicy"`
	MountPolicy       string   `json:"mountPolicy"`
	LifecycleOwner    string   `json:"lifecycleOwner"`
	DeployPolicy      string   `json:"deployPolicy"`
	RollbackPolicy    string   `json:"rollbackPolicy"`
	RemovePolicy      string   `json:"removePolicy"`
	PurgePolicy       string   `json:"purgePolicy"`
	BackupPolicy      string   `json:"backupPolicy"`
	RestorePolicy     string   `json:"restorePolicy"`
	MigrationVersion  int      `json:"migrationVersion"`
	DeprecatedRoots   []string `json:"deprecatedRoots"`
}

// Binding attaches the immutable runtime contract to one exact production
// source, artifact, deployment and external instance.
type Binding struct {
	SchemaVersion    int                 `json:"schemaVersion"`
	Contract         RuntimeRootContract `json:"contract"`
	ContractRevision string              `json:"contractRevision"`
	InstanceID       string              `json:"instanceId"`
	SourceRevision   string              `json:"sourceRevision"`
	ArtifactRevision string              `json:"artifactRevision"`
	DeploymentID     string              `json:"deploymentId"`
	BindingRevision  string              `json:"bindingRevision"`
}

// Installed returns the canonical Linux production contract. A change to any
// field changes ContractRevision and must be treated as a migration decision.
func Installed() RuntimeRootContract {
	runtimeRoot := path.Join(InstalledRoot, ".runtime", "server-protection")
	return RuntimeRootContract{
		SchemaVersion:     SchemaVersion,
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
		MountPolicy:       "reject-bind-or-separate-mount",
		LifecycleOwner:    "server-protection-component",
		DeployPolicy:      "preserve",
		RollbackPolicy:    "preserve",
		RemovePolicy:      "preserve",
		PurgePolicy:       "remove-with-application-install-root",
		BackupPolicy:      "database-backup-preserves-runtime-root-in-place",
		RestorePolicy:     "database-restore-preserves-runtime-root-in-place",
		MigrationVersion:  MigrationVersion,
		DeprecatedRoots:   []string{DeprecatedRoot},
	}
}

// RootForDatabaseFolder retains the component's existing configurable test and
// development layout while sharing the production relative-root contract.
func RootForDatabaseFolder(databaseFolder string) string {
	// The hardened native profile deliberately moves the database into
	// systemd-managed state while the component's separately versioned runtime
	// contract remains under the immutable application identity root. Keep this
	// mapping exact so development and custom database folders retain their
	// existing adjacent .runtime layout.
	if path.Clean(filepath.ToSlash(databaseFolder)) == path.Join(NativeDataRoot, "db") {
		return Installed().RuntimeRoot
	}
	return filepath.Join(filepath.Dir(databaseFolder), ".runtime", "server-protection")
}

func (c RuntimeRootContract) Validate() error {
	if c.SchemaVersion != SchemaVersion || c.ComponentID != "server-protection" {
		return errors.New("runtime root contract schema or component is unsupported")
	}
	if !canonicalLinuxAbsolute(c.RuntimeRoot) || c.RuntimeRoot != path.Join(InstalledRoot, ".runtime", "server-protection") {
		return errors.New("runtime root contract does not name the canonical installed root")
	}
	if c.ArtifactRoot != c.RuntimeRoot || c.HelperManagedRoot != c.RuntimeRoot ||
		c.RevisionRoot != path.Join(c.RuntimeRoot, "revisions") ||
		c.OperationsRoot != path.Join(c.RuntimeRoot, "operations") ||
		c.RecoveryRoot != path.Join(c.RuntimeRoot, "recovery") {
		return errors.New("runtime root contract subroots are inconsistent")
	}
	if c.OwnerIdentity != "solovey-ui-service-account" || c.MutationAuthority != "solovey-privileged-broker" || c.DirectoryMode != 0o700 ||
		c.SymlinkPolicy != "reject" || c.MountPolicy != "reject-bind-or-separate-mount" {
		return errors.New("runtime root contract ownership or path policy is unsafe")
	}
	if c.LifecycleOwner != "server-protection-component" || c.DeployPolicy != "preserve" ||
		c.RollbackPolicy != "preserve" || c.RemovePolicy != "preserve" ||
		c.PurgePolicy != "remove-with-application-install-root" ||
		c.BackupPolicy != "database-backup-preserves-runtime-root-in-place" ||
		c.RestorePolicy != "database-restore-preserves-runtime-root-in-place" ||
		c.MigrationVersion != MigrationVersion {
		return errors.New("runtime root contract lifecycle policy is inconsistent")
	}
	if len(c.DeprecatedRoots) != 1 || c.DeprecatedRoots[0] != DeprecatedRoot || c.DeprecatedRoots[0] == c.RuntimeRoot {
		return errors.New("runtime root contract deprecated-root policy is inconsistent")
	}
	return nil
}

func (c RuntimeRootContract) Revision() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func Bind(contract RuntimeRootContract, instanceID, sourceRevision, artifactRevision, deploymentID string) (Binding, error) {
	contractRevision, err := contract.Revision()
	if err != nil {
		return Binding{}, err
	}
	if !uuidPattern.MatchString(instanceID) || !sourcePattern.MatchString(sourceRevision) ||
		!artifactPattern.MatchString(artifactRevision) || !deployPattern.MatchString(deploymentID) {
		return Binding{}, errors.New("runtime root binding identity is malformed")
	}
	binding := Binding{
		SchemaVersion: BindingVersion, Contract: contract, ContractRevision: contractRevision,
		InstanceID: instanceID, SourceRevision: sourceRevision,
		ArtifactRevision: artifactRevision, DeploymentID: deploymentID,
	}
	revision, err := binding.revision()
	if err != nil {
		return Binding{}, err
	}
	binding.BindingRevision = revision
	return binding, nil
}

func (b Binding) Validate() error {
	if b.SchemaVersion != BindingVersion || !revisionPattern.MatchString(b.ContractRevision) ||
		!revisionPattern.MatchString(b.BindingRevision) {
		return errors.New("runtime root binding schema or revision is malformed")
	}
	contractRevision, err := b.Contract.Revision()
	if err != nil || contractRevision != b.ContractRevision {
		return errors.New("runtime root binding contract revision differs")
	}
	if !uuidPattern.MatchString(b.InstanceID) || !sourcePattern.MatchString(b.SourceRevision) ||
		!artifactPattern.MatchString(b.ArtifactRevision) || !deployPattern.MatchString(b.DeploymentID) {
		return errors.New("runtime root binding identity is malformed")
	}
	revision, err := b.revision()
	if err != nil || revision != b.BindingRevision {
		return errors.New("runtime root binding revision differs")
	}
	return nil
}

func (b Binding) revision() (string, error) {
	copy := b
	copy.BindingRevision = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func canonicalLinuxAbsolute(value string) bool {
	return value != "" && strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsRune(value, 0)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (c RuntimeRootContract) String() string {
	revision, err := c.Revision()
	if err != nil {
		return fmt.Sprintf("invalid runtime root contract: %v", err)
	}
	return c.RuntimeRoot + "@" + revision
}
