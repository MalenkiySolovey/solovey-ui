//go:build !minimal

package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
)

func storageRoot() string {
	return filepath.Join(configstorage.GetDBFolderPath(), "fallback-html")
}

func publishRoot(siteID uint, version string) string {
	return filepath.Join(storageRoot(), "publishes", "site-"+uintString(siteID), version)
}

func assetRoot(siteID uint) string {
	return filepath.Join(storageRoot(), "assets", "site-"+uintString(siteID))
}

func templateRoot(templateID string) string {
	return filepath.Join(storageRoot(), "templates", safeArchiveName(templateID))
}

func RemoveStorage() error {
	if root := storageRoot(); root != "" {
		return os.RemoveAll(root)
	}
	return nil
}

func RemoveSiteStorage(siteID uint) error {
	for _, root := range []string{
		assetRoot(siteID),
		filepath.Join(storageRoot(), "publishes", "site-"+uintString(siteID)),
	} {
		if root == "" {
			continue
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
	}
	return nil
}

func writeOwnedNewFile(root, target string, data []byte, perm fs.FileMode) error {
	if err := ensureOwnedDir(root, filepath.Dir(target)); err != nil {
		return err
	}
	if err := ensurePathInside(root, target); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if errors.Is(err, os.ErrExist) {
		if info, statErr := os.Lstat(target); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink %s", target)
		}
		return fmt.Errorf("refusing to overwrite existing storage file %s", target)
	}
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(target)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return closeErr
	}
	return nil
}

func readOwnedRegularFile(root, target string) ([]byte, error) {
	if err := ensurePathInside(root, target); err != nil {
		return nil, err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to read symlink %s", target)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to read non-regular storage file %s", target)
	}
	return os.ReadFile(target) // #nosec G304 -- path is validated against component-owned storage root.
}

func stageOwnedFileRemoval(root, target string) (string, bool, error) {
	if err := ensurePathInside(root, target); err != nil {
		return "", false, err
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("refusing to remove non-regular storage file %s", target)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".fallback-delete-*")
	if err != nil {
		return "", false, err
	}
	staged := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(staged)
		return "", false, err
	}
	if err := os.Remove(staged); err != nil {
		return "", false, err
	}
	if err := os.Rename(target, staged); err != nil {
		return "", false, err
	}
	return staged, true, nil
}

func ensureOwnedDir(root, targetDir string) error {
	rootAbs, targetAbs, err := cleanOwnedPaths(root, targetDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(rootAbs, 0o750); err != nil {
		return err
	}
	if err := rejectSymlink(rootAbs); err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	current := rootAbs
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing storage symlink directory %s", current)
			}
			if !info.IsDir() {
				return fmt.Errorf("storage path %s is not a directory", current)
			}
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(current, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			if err := rejectSymlink(current); err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

func ensurePathInside(root, target string) error {
	_, _, err := cleanOwnedPaths(root, target)
	return err
}

func cleanOwnedPaths(root, target string) (string, string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", "", err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("storage path %s escapes root %s", target, root)
	}
	return rootAbs, targetAbs, nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing storage symlink %s", path)
	}
	return nil
}
