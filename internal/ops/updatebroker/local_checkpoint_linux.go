//go:build linux

package updatebroker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func localCheckpointTarget(name string, sequence uint64, digest string, local LocalGenerationCheckpointV1) bool {
	return ValidRollbackTarget(sequence, digest, local) && (local == (LocalGenerationCheckpointV1{}) ||
		strings.HasPrefix(name, "installer-") && releaseDirectoryPattern.MatchString(name))
}

func (h *Host) installerCheckpoint(name string) (LocalGenerationCheckpointV1, error) {
	failed := errors.New("installed generation checkpoint is unavailable or changed")
	if !strings.HasPrefix(name, "installer-") || !releaseDirectoryPattern.MatchString(name) {
		return LocalGenerationCheckpointV1{}, failed
	}
	root := filepath.Join(h.ReleaseRoot, name)
	// An existing malformed or lost signed identity can never fall back to a
	// locally classified checkpoint. Only the installer's own namespace qualifies.
	if _, err := os.Lstat(filepath.Join(root, "release-identity.json")); !errors.Is(err, os.ErrNotExist) {
		return LocalGenerationCheckpointV1{}, failed
	}
	profile, err := releaseBinaryProfile(root)
	if err != nil || h.verifyPreparedReleaseExecutables(root, profile) != nil || verifyReleaseTree(root) != nil {
		return LocalGenerationCheckpointV1{}, failed
	}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	if err := encoder.Encode([]string{installerRootCheckpoint, name}); err != nil {
		return LocalGenerationCheckpointV1{}, failed
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		stat, ok := info.Sys().(*syscall.Stat_t)
		if err != nil || !ok {
			return failed
		}
		entry := struct {
			Path   string
			Mode   uint32
			GID    uint32
			Size   int64
			Digest string
		}{Path: filepath.ToSlash(relative), Mode: uint32(info.Mode()), GID: stat.Gid}
		if !info.IsDir() {
			entry.Size = info.Size()
			entry.Digest, err = fileDigest(path)
			if err != nil {
				return err
			}
		}
		return encoder.Encode(entry)
	})
	if err != nil || verifyReleaseTree(root) != nil {
		return LocalGenerationCheckpointV1{}, failed
	}
	return LocalGenerationCheckpointV1{Kind: installerRootCheckpoint, Digest: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (h *Host) localCheckpointMatches(name string, checkpoint LocalGenerationCheckpointV1) bool {
	if checkpoint == (LocalGenerationCheckpointV1{}) {
		return true
	}
	actual, err := h.installerCheckpoint(name)
	return err == nil && actual == checkpoint
}
