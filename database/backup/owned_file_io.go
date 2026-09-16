package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ReadOwnedFile reads one bounded regular file inside an owner-selected root.
// Symlinks in either the root or path are rejected. It never follows a path
// supplied solely by an untrusted archive.
func ReadOwnedFile(root, name string) ([]byte, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	name, err = filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, name)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return nil, errors.New("file is outside owner root")
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil || resolved != name {
		return nil, errors.New("owner file path is unavailable or indirect")
	}
	before, err := os.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > MaxOwnerFileBytes {
		return nil, errors.New("owner file is not bounded regular data")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, MaxOwnerFileBytes+1))
	after, statErr := f.Stat()
	closeErr := f.Close()
	if readErr != nil || statErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, statErr, closeErr)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(data)) != after.Size() {
		return nil, errors.New("owner file changed while reading")
	}
	return data, nil
}

// PublishOwnedFile writes a content-addressed immutable file under a semantic
// owner's restore directory. A prior file is accepted only if its bytes match.
// Failed later DB restore leaves at most unreferenced content, never changes
// the files referenced by the rollback database.
func PublishOwnedFile(root string, data []byte, publish bool) (string, error) {
	if len(data) > MaxOwnerFileBytes {
		return "", errors.New("owner file exceeds bound")
	}
	sum := sha256.Sum256(data)
	name := filepath.Join(root, hex.EncodeToString(sum[:]))
	if !publish {
		return name, nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// Create one component at a time, checking before descending. Never follow
	// a pre-existing symlink through MkdirAll.
	missing := []string{}
	for current := abs; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, current)
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("owner directory is unsafe")
		}
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil || resolved != current {
			return "", errors.New("owner directory is indirect")
		}
		break
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil {
			return "", err
		}
		if err := syncRestoreRecoveryDirectory(filepath.Dir(missing[i])); err != nil {
			return "", err
		}
	}
	if _, err := os.Lstat(name); err == nil {
		existing, err := ReadOwnedFile(root, name)
		if err != nil || !bytes.Equal(existing, data) {
			return "", errors.New("immutable owner file conflicts")
		}
		return name, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	f, err := os.CreateTemp(root, ".incoming-*")
	if err != nil {
		return "", err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	_, writeErr := f.Write(data)
	syncErr, closeErr := f.Sync(), f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return "", err
	}
	// Link publishes without replacing a concurrently created object.
	if err := os.Link(temporary, name); err != nil {
		existing, readErr := ReadOwnedFile(root, name)
		if readErr != nil || !bytes.Equal(existing, data) {
			return "", errors.Join(err, readErr)
		}
	}
	if err := os.Remove(temporary); err != nil {
		return "", err
	}
	return name, syncRestoreRecoveryDirectory(root)
}
