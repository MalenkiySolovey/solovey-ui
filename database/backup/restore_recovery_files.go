package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type restoreRecoveryFileOps struct {
	mkdirAll      func(string, fs.FileMode) error
	stat          func(string) (os.FileInfo, error)
	open          func(string) (restoreRecoveryReadFile, error)
	createTemp    func(string, string) (restoreRecoveryTempFile, error)
	remove        func(string) error
	rename        func(string, string) error
	syncDirectory func(string) error
}

type restoreRecoveryReadFile interface {
	io.Reader
	io.Closer
}

type restoreRecoveryTempFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

var productionRestoreRecoveryFileOps = restoreRecoveryFileOps{
	mkdirAll: os.MkdirAll, stat: os.Stat,
	open: func(path string) (restoreRecoveryReadFile, error) { return os.Open(path) },
	createTemp: func(directory, pattern string) (restoreRecoveryTempFile, error) {
		return os.CreateTemp(directory, pattern)
	},
	remove: os.Remove, rename: os.Rename, syncDirectory: syncRestoreRecoveryDirectory,
}

func ensureRestoreRecoveryDirectory(directory string, ops restoreRecoveryFileOps) error {
	if directory == "" || ops.mkdirAll == nil || ops.stat == nil || ops.syncDirectory == nil {
		return errors.New("restore recovery filesystem is unavailable")
	}
	missing := make([]string, 0, 4)
	for current := filepath.Clean(directory); ; current = filepath.Dir(current) {
		info, err := ops.stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("restore recovery path %s is not a directory", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return errors.New("restore recovery directory has no existing ancestor")
		}
	}
	if err := ops.mkdirAll(directory, 0o700); err != nil {
		return err
	}
	for index := len(missing) - 1; index >= 0; index-- {
		if err := ops.syncDirectory(filepath.Dir(missing[index])); err != nil {
			return err
		}
	}
	return nil
}

func verifyRestoreRecoveryFile(ctx context.Context, path, digest string, ops restoreRecoveryFileOps) (int64, error) {
	if ctx == nil || len(digest) != 64 || ops.open == nil {
		return 0, errors.New("restore recovery verification input is invalid")
	}
	file, err := ops.open(path)
	if err != nil {
		return 0, err
	}
	hash := sha256.New()
	written, copyErr := copyContext(ctx, hash, io.LimitReader(file, MaxRestoreBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written <= 0 || written > MaxRestoreBytes || hex.EncodeToString(hash.Sum(nil)) != digest {
		return 0, errors.Join(copyErr, closeErr, errors.New("pre-restore recovery backup failed strict reopen/hash validation"))
	}
	return written, nil
}
