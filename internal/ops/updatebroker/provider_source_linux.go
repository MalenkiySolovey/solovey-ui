//go:build linux

package updatebroker

import (
	"errors"
	"path/filepath"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

// verifyPreparedProvider observes the same opened executable object before and
// after metadata inspection. It assigns no installation or runtime capability
// authority. Legacy generations have no v2 source declaration.
func verifyPreparedProvider(root string, identity ReleaseIdentityV1) error {
	if identity.CoreProvider == (release.CoreProviderSource{}) {
		return nil
	}
	object, err := openPreparedProvider(root, identity)
	if err != nil {
		return err
	}
	return object.Close()
}

// The semantic owner retains this same descriptor for a fixed target check.
// Neither the request nor build metadata supplies a command or executable path.
func openPreparedProvider(root string, identity ReleaseIdentityV1) (*executableobject.Object, error) {
	if ValidateRelease(identity) != nil || len(identity.Artifacts) != 1 {
		return nil, errors.New("prepared provider identity is incomplete")
	}
	object, err := executableobject.Open(filepath.Join(root, "solovey-ui"), executableobject.Policy{
		MaxBytes: release.MaxPanelMemberBytes, RequireRegular: true, RequireExecutable: true,
		RequireRootOwner: true, RequiredMode: 0o755, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022,
		AllowRootOwnedStickyAncestry: true, RequireStablePath: true,
	})
	if err != nil {
		return nil, errors.New("prepared provider executable object is unsafe")
	}
	observed, inspectErr := release.InspectPanelBuild(object.File(), identity.CoreProvider)
	artifact := identity.Artifacts[0]
	if inspectErr != nil || observed.CoreProvider != identity.CoreProvider || observed.BinaryProfile != identity.BinaryProfile ||
		observed.Platform != artifact.Platform || observed.Arch != artifact.Arch || object.Revalidate() != nil {
		_ = object.Close()
		return nil, errors.New("prepared executable differs from its provider, profile or target identity")
	}
	return object, nil
}
