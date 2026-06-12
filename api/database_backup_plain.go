package api

import (
	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) getPlainDb(c *gin.Context, request databaseBackupRequest) {
	db, err := database.GetDb(request.Exclude)
	if err != nil {
		a.recordAudit(c, requestActor(c), "db_export_failed", "database", service.AuditSeverityWarn, map[string]any{
			"channel": "download",
		})
		jsonMsg(c, "", err)
		return
	}
	a.recordAudit(c, requestActor(c), "db_exported", "database", service.AuditSeverityWarn, map[string]any{
		"channel": "download",
		"exclude": request.Exclude,
	})
	// Real-time alert on config exfiltration (T1530): a full DB export is one of
	// the highest-signal admin-compromise events.
	a.TelegramService.NotifyTelegramEvent("db_exported", map[string]string{
		"actor": requestActor(c),
		"ip":    getRemoteIp(c),
	})
	writeDatabaseDownload(c, db, false)
}
