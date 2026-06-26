package dbtransfer

import (
	"io"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) getPlainDb(c *gin.Context, request databaseBackupRequest) {
	backupPath, cleanup, err := backup.PrepareExport(request.Exclude)
	if err != nil {
		a.Audit(c, a.Actor(c), "db_export_failed", "database", service.AuditSeverityWarn, map[string]any{
			"channel": "download",
		})
		a.JSONMsg(c, "", err)
		return
	}
	defer cleanup()

	backupFile, err := os.Open(backupPath) // #nosec G304 -- internal temporary path.
	if err != nil {
		a.Audit(c, a.Actor(c), "db_export_failed", "database", service.AuditSeverityWarn, map[string]any{
			"channel": "download",
		})
		a.JSONMsg(c, "", err)
		return
	}
	defer backupFile.Close()

	a.Audit(c, a.Actor(c), "db_exported", "database", service.AuditSeverityWarn, map[string]any{
		"channel": "download",
		"exclude": request.Exclude,
	})
	// Real-time alert on config exfiltration (T1530): a full DB export is one of
	// the highest-signal admin-compromise events.
	a.TelegramService.NotifyTelegramEvent("db_exported", map[string]string{
		"actor": a.Actor(c),
		"ip":    a.RemoteIP(c),
	})
	writeDatabaseDownloadHeaders(c, false)
	_, _ = io.Copy(c.Writer, backupFile)
}
