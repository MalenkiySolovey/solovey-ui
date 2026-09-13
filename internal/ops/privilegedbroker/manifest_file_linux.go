//go:build linux

package privilegedbroker

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
)

func readTrustedManifest(path string) ([]byte, error) {
	if err := trustedManifestAncestry(path); err != nil {
		return nil, err
	}
	object, err := openTrustedManifestObject(path)
	if err != nil {
		return nil, err
	}
	defer object.Close()
	return readTrustedManifestObject(object)
}

func openTrustedManifestObject(path string) (*executableobject.Object, error) {
	return executableobject.Open(path, executableobject.Policy{
		MaxBytes: 256 << 10, RequireRegular: true, RequireRootOwner: true, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022,
		AllowRootOwnedStickyAncestry: true,
		RequireStablePath:            true,
	})
}

// readTrustedManifestObject completes one descriptor-bound manifest read
// transaction. Keeping this step separate makes the authentication/read/
// revalidation boundary explicit and lets tests deterministically replace the
// path after authentication without adding a production test hook.
func readTrustedManifestObject(object *executableobject.Object) ([]byte, error) {
	if object == nil || object.File() == nil {
		return nil, errors.New("broker client manifest object is unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(object.File(), (256<<10)+1))
	if err != nil || len(data) == 0 || len(data) > 256<<10 {
		return nil, errors.New("broker client manifest exceeds its bound")
	}
	if Digest(data) != object.Identity().Digest {
		return nil, errors.New("broker client manifest changed while reading")
	}
	if err := object.Revalidate(); err != nil {
		return nil, errors.New("broker client manifest changed while reading")
	}
	return data, nil
}

// trustedManifestAncestry authenticates the fixed logical label's lexical
// ancestry without confusing it with the canonical filesystem ancestry. A
// root-owned, non-writable platform alias (for example OpenWrt's /run link)
// is an allowed label component; the resolved target is authenticated
// separately by executableobject.Open. This keeps arbitrary aliases unsafe:
// an alias below an untrusted parent is rejected here, while an alias that
// resolves into an untrusted canonical hierarchy is rejected by the generic
// object policy.
func trustedManifestAncestry(path string) error {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return errors.New("broker client manifest ancestry is unsafe")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return errors.New("broker client manifest ancestry ownership is unsafe")
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
				return errors.New("broker client manifest ancestry is unsafe")
			}
		}
		if current == string(filepath.Separator) {
			return nil
		}
	}
}
