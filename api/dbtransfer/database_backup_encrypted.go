package dbtransfer

import (
	"net/http"

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

	db, err := backup.Export(request.Exclude)
	if err != nil {
		a.Audit(c, a.Actor(c), "db_export_failed", "database", service.AuditSeverityWarn, map[string]any{
			"channel":   "local_download",
			"encrypted": true,
		})
		a.JSONMsg(c, "", err)
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

func respondDatabaseBackupCodecError(c *gin.Context, err error, fallbackClass string) {
	status, class := backupCodecHTTPError(err, fallbackClass)
	respondDatabaseBackupError(c, status, class)
}
