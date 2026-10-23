//go:build linux

package updatebroker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Provider selections within one signed manifest have distinct immutable
// artifact sets. The full selection digest prevents their generation names
// from colliding; sequence high-water admission remains a separate owner.
func providerReleaseDirectoryName(value ReleaseIdentityV1) string {
	return fmt.Sprintf("%020d-%s-%s", value.Sequence, value.ManifestDigest[:16], value.ArtifactSetDigest)
}

// Control requests recovered from operation rows carry the exact manifest and
// selected-artifact digests. Resolve the stored source declaration through that
// identity; do not invent a provider when a request omits its full projection.
// Legacy prepared generations retain their original labels and remain usable.
func (h *Host) resolveReleaseGeneration(value ReleaseIdentityV1) (string, ReleaseIdentityV1, error) {
	if h == nil || ValidateRelease(value) != nil {
		return "", ReleaseIdentityV1{}, errors.New("release generation request is invalid")
	}
	names := []string{providerReleaseDirectoryName(value), fmt.Sprintf("%020d-%s", value.Sequence, value.ManifestDigest[:16])}
	var matched string
	var identity ReleaseIdentityV1
	for _, name := range names {
		root := filepath.Join(h.ReleaseRoot, name)
		info, err := os.Lstat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", ReleaseIdentityV1{}, errors.New("release generation namespace is unsafe")
		}
		stored, err := loadReleaseIdentity(root)
		if err != nil {
			return "", ReleaseIdentityV1{}, errors.New("release generation metadata is unavailable")
		}
		if !sameReleaseIdentity(stored, value, false) {
			continue
		}
		if matched != "" {
			return "", ReleaseIdentityV1{}, errors.New("release generation identity is ambiguous")
		}
		matched, identity = name, stored
	}
	if matched == "" {
		return "", ReleaseIdentityV1{}, errors.New("bound release generation is unavailable")
	}
	return matched, identity, nil
}
