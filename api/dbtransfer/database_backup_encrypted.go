package dbtransfer

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func (a *Handler) getEncodedDb(c *gin.Context, request databaseBackupRequest, codec backupExportCodecEntry) {
	if codec.codec.Preflight != nil {
		if err := codec.codec.Preflight(c); err != nil {
			respondDatabaseBackupCodecError(c, err, "preflight_failed")
			return
		}
	}

	backupPath, cleanup, err := backup.PrepareExportContext(c.Request.Context(), request.Exclude)
	if err != nil {
		a.Audit(c, a.Actor(c), "db_export_failed", "database", service.AuditSeverityWarn, map[string]any{
			"channel":   "local_download",
			"encrypted": true,
		})
		a.JSONMsg(c, "", err)
		return
	}
	defer cleanup()
	plain, err := os.Open(backupPath) // #nosec G304 -- internal generated backup path.
	if err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "snapshot_open_failed")
		return
	}
	defer plain.Close()
	if codec.codec.EncodeStream != nil {
		a.getStreamEncodedDb(c, request, codec, backupPath, plain)
		return
	}
	const maxLegacyCodecMemory = int64(32 << 20)
	info, err := plain.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > maxLegacyCodecMemory {
		respondDatabaseBackupError(c, http.StatusRequestEntityTooLarge, "legacy_codec_memory_bound")
		return
	}
	db, err := io.ReadAll(io.LimitReader(plain, maxLegacyCodecMemory+1))
	if err != nil || int64(len(db)) > maxLegacyCodecMemory {
		common.WipeBytes(db)
		respondDatabaseBackupError(c, http.StatusRequestEntityTooLarge, "legacy_codec_memory_bound")
		return
	}
	defer common.WipeBytes(db)

	result, err := codec.codec.Encode(BackupExportContext{
		Gin:     c,
		Exclude: request.Exclude,
		Plain:   db,
	})
	if err != nil {
		respondDatabaseBackupCodecError(c, err, "encryption_failed")
		return
	}
	if len(result.Payload) == 0 {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "empty_payload")
		return
	}
	defer common.WipeBytes(result.Payload)

	auditEvent := result.AuditEvent
	if auditEvent == "" {
		auditEvent = "db_exported"
	}
	auditSeverity := result.AuditSeverity
	if auditSeverity == "" {
		auditSeverity = service.AuditSeverityInfo
	}
	a.Audit(c, a.Actor(c), auditEvent, "database", auditSeverity, result.AuditDetails)
	writeDatabaseDownload(c, result.Payload, result.Encrypted)
}

func (a *Handler) getStreamEncodedDb(c *gin.Context, request databaseBackupRequest, codec backupExportCodecEntry, backupPath string, plain *os.File) {
	destination, err := os.CreateTemp(filepath.Dir(backupPath), "s-ui-backup-encrypted-*.partial")
	if err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "encryption_staging_failed")
		return
	}
	destinationPath := destination.Name()
	defer func() {
		_ = destination.Close()
		_ = os.Remove(destinationPath)
	}()
	if err := destination.Chmod(0o600); err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "encryption_staging_failed")
		return
	}
	result, err := codec.codec.EncodeStream(BackupExportStreamContext{Gin: c, Exclude: request.Exclude,
		Plain: plain, Destination: destination})
	if err != nil {
		respondDatabaseBackupCodecError(c, err, "encryption_failed")
		return
	}
	if err := destination.Sync(); err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "encryption_staging_failed")
		return
	}
	info, err := destination.Stat()
	if err != nil || info.Size() <= 0 || result.PayloadBytes != 0 && result.PayloadBytes != info.Size() {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "empty_payload")
		return
	}
	if _, err := destination.Seek(0, io.SeekStart); err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "encryption_staging_failed")
		return
	}
	auditEvent := result.AuditEvent
	if auditEvent == "" {
		auditEvent = "db_exported"
	}
	auditSeverity := result.AuditSeverity
	if auditSeverity == "" {
		auditSeverity = service.AuditSeverityInfo
	}
	a.Audit(c, a.Actor(c), auditEvent, "database", auditSeverity, result.AuditDetails)
	writeDatabaseDownloadHeaders(c, result.Encrypted)
	_, _ = io.Copy(c.Writer, destination)
}

func respondDatabaseBackupCodecError(c *gin.Context, err error, fallbackClass string) {
	status, class := backupCodecHTTPError(err, fallbackClass)
	respondDatabaseBackupError(c, status, class)
}
