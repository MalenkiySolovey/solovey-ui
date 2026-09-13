package openwrt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

const (
	DatabaseDurabilitySchemaV3                  = "solovey-ui/openwrt-database-durability/v3"
	DefaultDurabilityProofPath                  = "/etc/solovey-ui/openwrt-database-durability.json"
	MaxDurabilityProofBytes                     = 64 << 10
	PersistenceProven          PersistenceState = "PERSISTENCE_PROVEN"
	VolatileProven             PersistenceState = "VOLATILE_PROVEN"
	PersistenceUnknown         PersistenceState = "UNKNOWN"
	DirectPersistentMount                       = "DIRECT_PERSISTENT_MOUNT"
	PinnedFSToolsOverlay                        = "PINNED_FSTOOLS_OVERLAY"
)

type PersistenceState string

// PersistenceAuthority is a deployment-selected projection of platform provenance.
// The reference/release owner generates it; durability only consumes it.
type PersistenceAuthority struct {
	OpenWrtRelease string `json:"openwrtRelease"`
	OpenWrtSource  string `json:"openwrtSource"`
	FSToolsSource  string `json:"fstoolsSource"`
}

// DatabaseDurabilityProofV3 proves storage admission, not completion of a
// package transaction or hardware power-loss safety. Storage writers retain
// ownership of their file/database publication barriers.
type DatabaseDurabilityProofV3 struct {
	Schema         string                  `json:"schema"`
	Revision       string                  `json:"revision"`
	DatabaseFolder string                  `json:"databaseFolder"`
	Persistence    PersistenceState        `json:"persistence"`
	ProofClass     string                  `json:"proofClass"`
	DatabaseMount  mountevidence.Fact      `json:"databaseMount"`
	Overlay        *FSToolsOverlayEvidence `json:"overlay,omitempty"`
}

type FSToolsOverlayEvidence struct {
	Authority    PersistenceAuthority           `json:"authority"`
	BackingMount mountevidence.Fact             `json:"backingMount"`
	UpperLabel   mountevidence.OverlayLabelFact `json:"upperLabel"`
	WorkLabel    mountevidence.OverlayLabelFact `json:"workLabel"`
}

type mountObserver func(string) (mountevidence.Fact, error)
type overlayLabelObserver func(string) (mountevidence.OverlayLabelFact, error)

// No physical-probe capability is injected into package/service admission.
// Unknown topology fails without attempting kernel writeback.
type durabilityEnvironment struct {
	Observe              mountObserver
	ObserveLabel         overlayLabelObserver
	PersistenceAuthority PersistenceAuthority
}

func (proof DatabaseDurabilityProofV3) Validate() error {
	if proof.Schema != DatabaseDurabilitySchemaV3 || proof.DatabaseFolder != DefaultDatabaseFolder ||
		proof.Persistence != PersistenceProven || proof.DatabaseMount.Validate() != nil ||
		proof.DatabaseMount.Target != proof.DatabaseFolder || !proof.DatabaseMount.Writable() {
		return ErrUnprovenDurableState
	}
	switch proof.ProofClass {
	case DirectPersistentMount:
		if proof.Overlay != nil || !durableWritableMount(proof.DatabaseMount) {
			return ErrUnprovenDurableState
		}
	case PinnedFSToolsOverlay:
		if classifyFSToolsOverlay(proof.DatabaseMount, proof.Overlay) != PersistenceProven {
			return ErrUnprovenDurableState
		}
	default:
		return ErrUnprovenDurableState
	}
	if proof.Revision == "" || proof.Revision != proof.revision() {
		return ErrUnprovenDurableState
	}
	return nil
}

func (proof *DatabaseDurabilityProofV3) seal() error {
	if proof == nil {
		return ErrUnprovenDurableState
	}
	proof.Schema = DatabaseDurabilitySchemaV3
	proof.Revision = proof.revision()
	return proof.Validate()
}

func (proof DatabaseDurabilityProofV3) revision() string {
	proof.Revision = ""
	data, _ := json.Marshal(proof)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func createDatabaseDurabilityProof(databaseFolder string, environment durabilityEnvironment) (DatabaseDurabilityProofV3, error) {
	if databaseFolder != DefaultDatabaseFolder || environment.Observe == nil {
		return DatabaseDurabilityProofV3{}, ErrUnprovenDurableState
	}
	primary, err := environment.Observe(databaseFolder)
	if err != nil {
		return DatabaseDurabilityProofV3{}, errors.Join(ErrUnprovenDurableState, err)
	}
	proof := DatabaseDurabilityProofV3{DatabaseFolder: databaseFolder, DatabaseMount: primary, Persistence: PersistenceProven}
	if primary.Filesystem == "overlay" {
		proof.ProofClass = PinnedFSToolsOverlay
		proof.Overlay, err = observeFSToolsOverlay(primary, environment)
		if err != nil {
			return DatabaseDurabilityProofV3{}, err
		}
	} else {
		proof.ProofClass = DirectPersistentMount
	}
	if err := proof.seal(); err != nil {
		return DatabaseDurabilityProofV3{}, err
	}
	return proof, nil
}

func observeFSToolsOverlay(primary mountevidence.Fact, environment durabilityEnvironment) (*FSToolsOverlayEvidence, error) {
	failure := func(state PersistenceState, cause error) (*FSToolsOverlayEvidence, error) {
		return nil, errors.Join(ErrUnprovenDurableState, fmt.Errorf("proof class %s: %s", PinnedFSToolsOverlay, state), cause)
	}
	if !validOverlayMount(primary) || environment.Observe == nil || environment.ObserveLabel == nil ||
		environment.PersistenceAuthority != selectedPersistenceAuthority() {
		return failure(PersistenceUnknown, nil)
	}
	if state := pinnedFSToolsPersistence(primary); state != PersistenceProven {
		return failure(state, nil)
	}
	evidence := &FSToolsOverlayEvidence{Authority: environment.PersistenceAuthority}
	var err error
	evidence.BackingMount, err = environment.Observe("/overlay")
	if err != nil {
		return failure(PersistenceUnknown, err)
	}
	evidence.UpperLabel, err = environment.ObserveLabel(primary.OptionValue("upperdir"))
	if err != nil {
		return failure(PersistenceUnknown, err)
	}
	evidence.WorkLabel, err = environment.ObserveLabel(primary.OptionValue("workdir"))
	if err != nil {
		return failure(PersistenceUnknown, err)
	}
	if state := classifyFSToolsOverlay(primary, evidence); state != PersistenceProven {
		return failure(state, nil)
	}
	// Fence both mount identities across observation. Consumers obtain fresh
	// evidence again; saved label visibility is never live backing authority.
	backing, err := environment.Observe("/overlay")
	if err != nil || backing.Revision != evidence.BackingMount.Revision {
		return failure(PersistenceUnknown, err)
	}
	current, err := environment.Observe(primary.Target)
	if err != nil || current.Revision != primary.Revision {
		return failure(PersistenceUnknown, err)
	}
	return evidence, nil
}

func recheckDatabaseDurabilityProof(proof DatabaseDurabilityProofV3, required uint64, environment durabilityEnvironment,
	available func(string) (uint64, error)) (DurableStateEvidence, error) {
	if proof.Validate() != nil || required == 0 || available == nil {
		return DurableStateEvidence{}, ErrUnprovenDurableState
	}
	current, err := createDatabaseDurabilityProof(proof.DatabaseFolder, environment)
	if err != nil || current.DatabaseMount.Revision != proof.DatabaseMount.Revision || current.ProofClass != proof.ProofClass {
		return DurableStateEvidence{}, errors.Join(ErrUnprovenDurableState, err)
	}
	if proof.Overlay != nil && current.Overlay.BackingMount.Revision != proof.Overlay.BackingMount.Revision {
		return DurableStateEvidence{}, ErrUnprovenDurableState
	}
	bytes, err := available(proof.DatabaseFolder)
	if err != nil {
		return DurableStateEvidence{}, err
	}
	evidence := DurableStateEvidence{MountPath: proof.DatabaseMount.MountPoint, Persistent: true, AvailableBytes: bytes, RequiredBytes: required}
	if err := DefaultProfile().ValidateDurableState(evidence); err != nil {
		return DurableStateEvidence{}, err
	}
	return evidence, nil
}

func validOverlayMount(fact mountevidence.Fact) bool {
	upper, work := fact.OptionValue("upperdir"), fact.OptionValue("workdir")
	return fact.Validate() == nil && fact.Filesystem == "overlay" && fact.FilesystemMagic == 0x794c7630 && fact.Writable() &&
		canonicalProofPath(upper) && canonicalProofPath(work) && upper != work &&
		!fact.HasOption("volatile") && !strings.EqualFold(fact.OptionValue("fsync"), "volatile")
}

func durableWritableMount(fact mountevidence.Fact) bool {
	return fact.Validate() == nil && fact.Writable() && ApprovedPersistentFilesystem(fact.Filesystem)
}

func classifyFSToolsOverlay(primary mountevidence.Fact, evidence *FSToolsOverlayEvidence) PersistenceState {
	if !validOverlayMount(primary) || primary.MountPoint != "/" || primary.Root != "/" ||
		primary.Target != primary.ResolvedTarget || evidence == nil || evidence.Authority != selectedPersistenceAuthority() {
		return PersistenceUnknown
	}
	if state := pinnedFSToolsPersistence(primary); state != PersistenceProven {
		return state
	}
	backing := evidence.BackingMount
	if backing.Validate() != nil || backing.Target != "/overlay" || backing.ResolvedTarget != "/overlay" ||
		backing.MountPoint != "/overlay" || backing.Root != "/" || backing.MountID == primary.MountID {
		return PersistenceUnknown
	}
	if volatileMount(backing) {
		return VolatileProven
	}
	if !durableWritableMount(backing) {
		return PersistenceUnknown
	}
	for _, item := range []struct {
		fact  mountevidence.OverlayLabelFact
		label string
	}{
		{evidence.UpperLabel, primary.OptionValue("upperdir")}, {evidence.WorkLabel, primary.OptionValue("workdir")},
	} {
		if item.fact.Validate() != nil || item.fact.Label != item.label {
			return PersistenceUnknown
		}
		if item.fact.Visibility == mountevidence.OverlayLabelVisible &&
			(item.fact.Mount.Target != item.label || item.fact.Mount.ResolvedTarget != item.label || !item.fact.Mount.SameMount(backing)) {
			return PersistenceUnknown
		}
	}
	return PersistenceProven
}

// Exact post-pivot labels emitted by the deployment's pinned fstools are only
// sufficient together with current, noncontradictory persistent backing facts.
func pinnedFSToolsPersistence(primary mountevidence.Fact) PersistenceState {
	upper, work := primary.OptionValue("upperdir"), primary.OptionValue("workdir")
	switch {
	case primary.Source == "overlayfs:/overlay" && upper == "/overlay/upper" && work == "/overlay/work":
		return PersistenceProven
	case primary.Source == "overlayfs:/tmp/root" && upper == "/tmp/root/upper" && work == "/tmp/root/work":
		return VolatileProven
	default:
		return PersistenceUnknown
	}
}

func volatileMount(fact mountevidence.Fact) bool {
	return fact.Validate() == nil && (fact.Filesystem == "tmpfs" || fact.Filesystem == "ramfs")
}

func canonicalProofPath(value string) bool {
	return value != "" && value != "/" && strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n\t")
}
