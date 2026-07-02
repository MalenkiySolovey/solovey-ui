package dbtransfer

import (
	"errors"
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) respondDatabaseImportResult(c *gin.Context, err error) {
	if err != nil {
		a.respondDatabaseImportFailure(c, err)
	} else {
		a.Audit(c, a.Actor(c), "db_imported", "database", service.AuditSeverityWarn, nil)
	}
	a.JSONMsg(c, "", err)
}

func (a *Handler) respondDatabaseImportFailure(c *gin.Context, err error) {
	a.Audit(c, a.Actor(c), "db_import_failed", "database", service.AuditSeverityWarn, map[string]any{
		"reason": databaseImportErrorClass(err),
	})
}

func (a *Handler) respondBackupRestoreDecryptionFailed(c *gin.Context, auditEvent string) {
	if auditEvent == "" {
		auditEvent = "backup_restore_decryption_failed"
	}
	a.Audit(c, a.Actor(c), auditEvent, "database", service.AuditSeverityWarn, map[string]any{
		"errorClass": "decryption_failed",
	})
	c.JSON(http.StatusBadRequest, gin.H{
		"success": false,
		"msg":     "restore: decryption_failed",
		"obj": gin.H{
			"errorClass": "decryption_failed",
		},
	})
}

func databaseImportErrorClass(err error) string {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return "too_large"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "format"), strings.Contains(msg, "sqlite"), strings.Contains(msg, "integrity"):
		return "invalid_db"
	default:
		return "failed"
	}
}
