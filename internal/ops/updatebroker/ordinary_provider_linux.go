//go:build linux

package updatebroker

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func verifyOrdinaryProviderPeer(peer broker.PeerIdentity, source release.CoreProviderSource, target ReleaseIdentityV1) error {
	if len(target.Artifacts) != 1 {
		return errors.New("ordinary update target is incomplete")
	}
	object, err := openProviderPeer(peer, source, target.BinaryProfile)
	if err != nil {
		return err
	}
	observed, inspectErr := release.InspectPanelBuild(object.File(), source)
	if inspectErr != nil || observed.Platform != target.Artifacts[0].Platform || observed.Arch != target.Artifacts[0].Arch || object.Revalidate() != nil {
		_ = object.Close()
		return errors.New("ordinary update target differs from the installed platform")
	}
	return object.Close()
}

// The same opened object and kernel peer tuple bind ordinary updates and
// explicit maintenance. Only the maintenance wrapper accepts transition input.
func openProviderPeer(peer broker.PeerIdentity, source release.CoreProviderSource, profile string) (*executableobject.Object, error) {
	failed := errors.New("installed provider source observation changed")
	if peer.PID <= 0 || release.ValidateCoreProviderSource(source) != nil {
		return nil, failed
	}
	object, err := executableobject.Open(peer.Executable, executableobject.Policy{
		MaxBytes: release.MaxPanelMemberBytes, RequireRegular: true, RequireExecutable: true,
		RequireRootOwner: true, RequiredMode: 0o755, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022,
		AllowRootOwnedStickyAncestry: true, RequireStablePath: true,
	})
	if err != nil {
		return nil, failed
	}
	identity := object.Identity()
	observed, err := release.InspectPanelBuild(object.File(), source)
	if err != nil || identity.Digest != peer.ExecutableDigest || identity.Device != peer.Device || identity.Inode != peer.Inode ||
		identity.Size != peer.ExecutableSize || observed.CoreProvider != source || observed.BinaryProfile != profile || object.Revalidate() != nil {
		_ = object.Close()
		return nil, failed
	}
	return object, nil
}

func validateOrdinaryUpdateState(state diskState) error {
	value := state.OrdinaryUpdate
	if value == (OrdinaryUpdateV1{}) {
		if state.PreparedPriorMaintenance != (providerMaintenanceState{}) {
			return errors.New("orphaned previous provider checkpoint")
		}
		return nil
	}
	failed := errors.New("ordinary update checkpoint is invalid")
	if state.ProviderMaintenance != (providerMaintenanceState{}) || ValidateOrdinaryUpdate(value) != nil {
		return failed
	}
	active := state.ActiveRelease == ordinaryUpdateReleaseName(value) && state.ActiveSequence == value.Sequence && state.ActiveDigest == value.ManifestDigest
	prepared := state.preparedRollbackMatches(value.OperationID, value.ManifestDigest, SemanticReference("rollback", value.OperationID, value.ManifestDigest)) &&
		state.ActiveRelease == state.PreparedRollbackRelease
	if !active && !prepared {
		return failed
	}
	if prior := state.PreparedPriorMaintenance; prior != (providerMaintenanceState{}) {
		if !state.hasBoundPreparedRollback() || (prior.Phase != "COMMITTED" && prior.Phase != "ROLLBACK_VERIFIED") {
			return failed
		}
		expected := prior.TargetProvider
		if prior.Phase == "ROLLBACK_VERIFIED" {
			expected = prior.Input.PreviousProvider
		}
		if expected != value.InstalledProvider {
			return failed
		}
		previous := state
		previous.ActiveRelease, previous.ActiveSequence, previous.ActiveDigest = state.PreparedRollbackRelease, state.PreparedRollbackSequence, state.PreparedRollbackDigest
		previous.ActiveLocalCheckpoint = state.PreparedRollbackLocalCheckpoint
		previous.VerifiedSequence, previous.VerifiedDigest = previous.ActiveSequence, previous.ActiveDigest
		previous.clearPreparedRollback()
		previous.ProviderMaintenance = prior
		if validateProviderMaintenanceState(previous) != nil {
			return failed
		}
	}
	return nil
}

func ordinaryUpdateReleaseName(value OrdinaryUpdateV1) string {
	return providerReleaseDirectoryName(ReleaseIdentityV1{Sequence: value.Sequence, ManifestDigest: value.ManifestDigest, ArtifactSetDigest: value.ArtifactSetDigest})
}

func (h *Host) ordinaryProviderVerificationAllowed(state diskState) bool {
	if state.OrdinaryUpdate == (OrdinaryUpdateV1{}) || validateOrdinaryUpdateState(state) != nil {
		return false
	}
	root := filepath.Join(h.ReleaseRoot, state.ActiveRelease)
	identity, err := loadReleaseIdentity(root)
	expected := state.OrdinaryUpdate.InstalledProvider
	if state.ActiveRelease == ordinaryUpdateReleaseName(state.OrdinaryUpdate) {
		expected = state.OrdinaryUpdate.CoreProvider
	}
	return err == nil && identity.CoreProvider == expected &&
		identity.BinaryProfile == state.OrdinaryUpdate.BinaryProfile && verifyPreparedProvider(root, identity) == nil
}

// Rollback follows the existing authenticated checkpoint and verifies the
// predecessor's exact source against its executable. It may restore an older
// engine source within this provider. Legacy/installer metadata stays unchanged.
func (h *Host) restoreOrdinaryProviderBinding(state *diskState, operationID string) error {
	if state.OrdinaryUpdate == (OrdinaryUpdateV1{}) {
		return nil
	}
	root := filepath.Join(h.ReleaseRoot, state.ActiveRelease)
	if _, err := os.Lstat(filepath.Join(root, "release-identity.json")); errors.Is(err, os.ErrNotExist) &&
		!(len(state.ActiveRelease) > 38 && releaseDirectoryPattern.MatchString(state.ActiveRelease)) {
		state.OrdinaryUpdate = OrdinaryUpdateV1{}
		return nil
	}
	identity, err := loadReleaseIdentity(root)
	if err != nil {
		return errors.New("restored ordinary update identity is unavailable")
	}
	if identity.CoreProvider == (release.CoreProviderSource{}) {
		state.OrdinaryUpdate = OrdinaryUpdateV1{}
		return nil
	}
	if identity.CoreProvider.ID != state.OrdinaryUpdate.InstalledProvider.ID || identity.BinaryProfile != state.OrdinaryUpdate.BinaryProfile ||
		verifyPreparedProvider(filepath.Join(h.ReleaseRoot, state.ActiveRelease), identity) != nil {
		return errors.New("restored ordinary update provider changed")
	}
	state.OrdinaryUpdate = ordinaryUpdateBinding(operationID, identity, identity.CoreProvider)
	return nil
}
