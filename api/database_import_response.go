package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) respondDatabaseImportResult(c *gin.Context, err error) {
	if err != nil {
		a.respondDatabaseImportFailure(c, err)
	} else {
		a.recordAudit(c, requestActor(c), "db_imported", "database", service.AuditSeverityWarn, nil)
	}
	jsonMsg(c, "", err)
}

func (a *ApiService) respondDatabaseImportFailure(c *gin.Context, err error) {
	a.recordAudit(c, requestActor(c), "db_import_failed", "database", service.AuditSeverityWarn, map[string]any{
		"reason": databaseImportErrorClass(err),
	})
}

func (a *ApiService) respondTelegramBackupRestoreDecryptionFailed(c *gin.Context) {
	a.recordAudit(c, requestActor(c), "tg_backup_restore_failed", "database", service.AuditSeverityWarn, map[string]any{
		"errorClass": "decryption_failed",
	})
	c.JSON(http.StatusBadRequest, Msg{
		Success: false,
		Msg:     "restore: decryption_failed",
		Obj: gin.H{
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
