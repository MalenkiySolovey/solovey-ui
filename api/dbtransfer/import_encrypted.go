package dbtransfer

import (
	"bytes"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func (a *Handler) prepareBackupCodecRestoreFile(c *gin.Context, source multipart.File, codec backupImportCodecEntry) (preparedDatabaseImportFile, bool) {
	if codec.codec.DecodeStream != nil {
		return a.prepareStreamBackupCodecRestoreFile(c, source, codec)
	}
	const maxLegacyCodecMemory = int64(32 << 20)
	payload, err := io.ReadAll(io.LimitReader(source, maxLegacyCodecMemory+1))
	if err == nil && int64(len(payload)) > maxLegacyCodecMemory {
		err = io.ErrShortBuffer
	}
	if err != nil {
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	defer common.WipeBytes(payload)

	plaintext, err := codec.codec.Decode(BackupImportContext{
		Gin:     c,
		Payload: payload,
	})
	if err != nil {
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	return preparedDatabaseImportFile{
		file: memoryMultipartFile{Reader: bytes.NewReader(plaintext)},
		cleanup: func() {
			_ = source.Close()
			common.WipeBytes(plaintext)
		},
	}, true
}

func (a *Handler) prepareStreamBackupCodecRestoreFile(c *gin.Context, source multipart.File, codec backupImportCodecEntry) (preparedDatabaseImportFile, bool) {
	directory := filepath.Join(configstorage.GetDBFolderPath(), "restore-staging")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	destination, err := os.CreateTemp(directory, "decoded-*.db")
	if err != nil {
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	path := destination.Name()
	cleanup := func() {
		_ = destination.Close()
		_ = source.Close()
		_ = os.Remove(path)
	}
	if err := destination.Chmod(0o600); err != nil {
		cleanup()
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	if err := codec.codec.DecodeStream(BackupImportStreamContext{Gin: c, Source: source, Destination: destination,
		MaxBytes: backup.MaxRestoreBytes}); err != nil {
		cleanup()
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	if err := destination.Sync(); err != nil {
		cleanup()
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	info, err := destination.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > backup.MaxRestoreBytes {
		cleanup()
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	if _, err := destination.Seek(0, io.SeekStart); err != nil {
		cleanup()
		a.respondBackupRestoreDecryptionFailed(c, codec.codec.FailureAuditEvent)
		return preparedDatabaseImportFile{}, false
	}
	return preparedDatabaseImportFile{file: destination, cleanup: cleanup}, true
}
