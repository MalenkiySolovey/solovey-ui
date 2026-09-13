package datalifecycle

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
	"strings"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

var (
	ErrRecoveryBackupUnavailable = errors.New("data lifecycle recovery backup is unavailable")
	ErrRecoveryBackupExpired     = errors.New("data lifecycle recovery backup horizon expired")
)

type recoveryFileOps struct {
	mkdirAll      func(string, fs.FileMode) error
	stat          func(string) (os.FileInfo, error)
	open          func(string) (recoveryReadFile, error)
	openConsumer  func(string) (*os.File, error)
	openFile      func(string, int, fs.FileMode) (recoveryWriteFile, error)
	remove        func(string) error
	rename        func(string, string) error
	readDir       func(string) ([]os.DirEntry, error)
	syncDirectory func(string) error
}

type recoveryReadFile interface {
	io.Reader
	io.Closer
}

type recoveryWriteFile interface {
	io.Writer
	Sync() error
	Close() error
}

var productionRecoveryFileOps = recoveryFileOps{
	mkdirAll: os.MkdirAll, stat: os.Stat,
	open:         func(path string) (recoveryReadFile, error) { return os.Open(path) },
	openConsumer: os.Open,
	openFile: func(path string, flag int, mode fs.FileMode) (recoveryWriteFile, error) {
		return os.OpenFile(path, flag, mode)
	},
	remove: os.Remove, rename: os.Rename, readDir: os.ReadDir, syncDirectory: syncRecoveryDirectory,
}

type RecoveryBackup struct {
	OperationID string `json:"operationId"`
	Kind        string `json:"kind"`
	BackupRef   string `json:"backupRef"`
	Bytes       int64  `json:"bytes"`
}

type RecoveryBackupFile interface {
	io.ReadSeeker
	io.Closer
}

type lockedRecoveryBackupFile struct {
	*os.File
	unlock func()
}

func (f *lockedRecoveryBackupFile) Close() error {
	err := f.File.Close()
	if f.unlock != nil {
		f.unlock()
		f.unlock = nil
	}
	return err
}

func (m *Manager) filesystem() recoveryFileOps {
	if m != nil && m.recoveryOps != nil {
		return *m.recoveryOps
	}
	return productionRecoveryFileOps
}

func (m *Manager) dropRecoveryRoot() string {
	if m != nil && strings.TrimSpace(m.Root) != "" {
		return filepath.Clean(m.Root)
	}
	return filepath.Join(configstorage.GetDBFolderPath(), "recovery", "drop-data")
}

func (m *Manager) restoreRecoveryRoot() string {
	if m != nil && strings.TrimSpace(m.RestoreRoot) != "" {
		return filepath.Clean(m.RestoreRoot)
	}
	return filepath.Join(configstorage.GetDBFolderPath(), "recovery", "restore")
}

func portableDropRecoveryFilename(operationID string) string {
	digest := sha256.Sum256([]byte(operationID))
	return "drop-" + hex.EncodeToString(digest[:]) + ".db"
}

func publishRecoveryFile(ctx context.Context, source io.Reader, destination string, limit int64, ops recoveryFileOps) (string, error) {
	if ctx == nil || source == nil || destination == "" || limit <= 0 || !ops.complete() {
		return "", errors.New("recovery publication input is invalid")
	}
	directory := filepath.Dir(destination)
	if err := ensureRecoveryDirectory(directory, ops); err != nil {
		return "", err
	}
	temporary := destination + ".partial"
	if err := ops.remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	output, err := ops.openFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	written, copyErr := copyWithContext(ctx, io.MultiWriter(output, hash), io.LimitReader(source, limit+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	digest := hex.EncodeToString(hash.Sum(nil))
	if copyErr != nil || syncErr != nil || closeErr != nil || written <= 0 || written > limit {
		removeErr := ops.remove(temporary)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		return "", errors.Join(copyErr, syncErr, closeErr, removeErr, errors.New("recovery backup exceeded bounds"))
	}
	if _, statErr := ops.stat(destination); statErr == nil {
		removeErr := ops.remove(temporary)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return "", removeErr
		}
		if _, err := verifyRecoveryFile(ctx, destination, digest, limit, ops); err != nil {
			return "", fmt.Errorf("recovery publication identity collision: %w", err)
		}
		return digest, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = ops.remove(temporary)
		return "", statErr
	}
	if err := ops.rename(temporary, destination); err != nil {
		_ = ops.remove(temporary)
		return "", err
	}
	if err := ops.syncDirectory(directory); err != nil {
		removeErr := ops.remove(destination)
		if removeErr == nil {
			removeErr = ops.syncDirectory(directory)
		}
		return "", errors.Join(err, removeErr)
	}
	if _, err := verifyRecoveryFile(ctx, destination, digest, limit, ops); err != nil {
		removeErr := ops.remove(destination)
		if removeErr == nil {
			removeErr = ops.syncDirectory(directory)
		}
		return "", errors.Join(err, removeErr)
	}
	return digest, nil
}

func (ops recoveryFileOps) complete() bool {
	return ops.mkdirAll != nil && ops.stat != nil && ops.open != nil && ops.openConsumer != nil && ops.openFile != nil && ops.remove != nil &&
		ops.rename != nil && ops.readDir != nil && ops.syncDirectory != nil
}

func ensureRecoveryDirectory(directory string, ops recoveryFileOps) error {
	missing := make([]string, 0, 4)
	for current := filepath.Clean(directory); ; current = filepath.Dir(current) {
		info, err := ops.stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("recovery path %s is not a directory", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return errors.New("recovery directory has no existing ancestor")
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

func verifyRecoveryFile(ctx context.Context, path, digest string, limit int64, ops recoveryFileOps) (int64, error) {
	if !validDigest(digest) || limit <= 0 {
		return 0, errors.New("recovery backup digest is invalid")
	}
	file, err := ops.open(path)
	if err != nil {
		return 0, err
	}
	hash := sha256.New()
	written, copyErr := copyWithContext(ctx, hash, io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written <= 0 || written > limit || hex.EncodeToString(hash.Sum(nil)) != digest {
		return 0, errors.Join(copyErr, closeErr, errors.New("recovery backup failed strict reopen/hash validation"))
	}
	return written, nil
}

func (m *Manager) recoveryArtifactCandidates(operation model.DataLifecycleOperation) []string {
	switch operation.Kind {
	case "DROP_DATA":
		return []string{
			filepath.Join(m.dropRecoveryRoot(), portableDropRecoveryFilename(operation.OperationID)),
			filepath.Join(m.dropRecoveryRoot(), operation.OperationID+".db"), // released Linux compatibility reader
		}
	case "RESTORE":
		return []string{filepath.Join(m.restoreRecoveryRoot(), "pre-restore-"+operation.BackupRef+".db")}
	default:
		return nil
	}
}

func (m *Manager) existingRecoveryArtifact(operation model.DataLifecycleOperation) (string, int64, error) {
	ops := m.filesystem()
	for _, candidate := range m.recoveryArtifactCandidates(operation) {
		info, err := ops.stat(candidate)
		if err == nil {
			if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > dbbackup.MaxRestoreBytes {
				return "", 0, ErrRecoveryBackupUnavailable
			}
			return candidate, info.Size(), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", 0, err
		}
	}
	return "", 0, ErrRecoveryBackupUnavailable
}

// OpenRecoveryBackup is the production consumer for an advertised BackupRef.
// It resolves only owner-derived paths, strictly reopens and hashes the file,
// and returns a seekable logical backup suitable for the existing rehearsal
// and restore pipeline.
func (m *Manager) OpenRecoveryBackup(ctx context.Context, operationID string) (RecoveryBackupFile, RecoveryBackup, error) {
	result := RecoveryBackup{}
	if m == nil || ctx == nil || !safeID(operationID, 96) || !strings.HasPrefix(operationID, "data-operation:") {
		return nil, result, errors.New("invalid recovery backup request")
	}
	m.mu.Lock()
	locked := true
	defer func() {
		if locked {
			m.mu.Unlock()
		}
	}()
	operation, err := m.Operation(ctx, operationID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, result, ErrRecoveryBackupExpired
	}
	if err != nil || !validDigest(operation.BackupRef) {
		return nil, result, errors.Join(ErrRecoveryBackupUnavailable, err)
	}
	path, _, err := m.existingRecoveryArtifact(operation)
	if err != nil {
		return nil, result, err
	}
	bytes, err := verifyRecoveryFile(ctx, path, operation.BackupRef, dbbackup.MaxRestoreBytes, m.filesystem())
	if err != nil {
		return nil, result, err
	}
	file, err := m.filesystem().openConsumer(path)
	if err != nil {
		return nil, result, err
	}
	result = RecoveryBackup{OperationID: operation.OperationID, Kind: operation.Kind, BackupRef: operation.BackupRef, Bytes: bytes}
	locked = false
	return &lockedRecoveryBackupFile{File: file, unlock: m.mu.Unlock}, result, nil
}
