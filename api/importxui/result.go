package importxui

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	dbimport "github.com/MalenkiySolovey/solovey-ui/database/importxui"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) recordImportSuccess(c *gin.Context, report *dbimport.Report, sha string) {
	a.Audit(c, a.Actor(c), "xui_import", "database", service.AuditSeverityInfo, reportAuditDetails(report, sha))
}

func (a *Handler) recordImportFailure(c *gin.Context, err error, sha string) {
	details := map[string]any{
		"reason": xuiImportErrorClass(err),
	}
	if sha != "" {
		details["sha256"] = sha
	}
	if errors.Is(err, dbimport.ErrBusy) {
		a.Audit(c, a.Actor(c), "xui_import_busy", "database", service.AuditSeverityWarn, details)
		return
	}
	a.Audit(c, a.Actor(c), "xui_import_failed", "database", service.AuditSeverityWarn, details)
}

func (a *Handler) recordRollbackInvalidBackup(c *gin.Context) {
	a.Audit(c, a.Actor(c), "xui_import_failed", "database", service.AuditSeverityWarn, map[string]any{"reason": "invalid_backup"})
}

func (a *Handler) recordRollbackSuccess(c *gin.Context, backupPath string) {
	a.Audit(c, a.Actor(c), "xui_import_rollback", "database", service.AuditSeverityWarn, map[string]any{
		"backup": filepath.Base(backupPath),
	})
}

func xuiImportError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	var maxBytesErr *http.MaxBytesError
	var fieldTooLargeErr *xuiFieldTooLargeError
	switch {
	case errors.As(err, &maxBytesErr):
		status = http.StatusRequestEntityTooLarge
	case errors.As(err, &fieldTooLargeErr):
		status = http.StatusRequestEntityTooLarge
	case strings.Contains(err.Error(), "request body too large"):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, dbimport.ErrBusy):
		status = http.StatusTooManyRequests
	case errors.Is(err, dbimport.ErrPlanStale) || strings.Contains(err.Error(), "plan_stale"):
		status = http.StatusBadRequest
	}
	c.JSON(status, Envelope{
		Success: false,
		Msg:     "import-xui: " + err.Error(),
	})
}

func xuiImportErrorClass(err error) string {
	var maxBytesErr *http.MaxBytesError
	var fieldTooLargeErr *xuiFieldTooLargeError
	switch {
	case errors.As(err, &maxBytesErr), errors.As(err, &fieldTooLargeErr), strings.Contains(err.Error(), "request body too large"):
		return "payload_too_large"
	case errors.Is(err, dbimport.ErrBusy), strings.Contains(err.Error(), "xui_import_busy"):
		return "busy"
	case errors.Is(err, dbimport.ErrPlanStale), strings.Contains(err.Error(), "plan_stale"):
		return "plan_stale"
	case strings.Contains(err.Error(), "not_sqlite"), strings.Contains(strings.ToLower(err.Error()), "sqlite"):
		return "not_sqlite"
	default:
		return "failed"
	}
}

func reportAuditDetails(report *dbimport.Report, sha string) map[string]any {
	details := summaryDetailsForAPI(report.Summary)
	if sha != "" {
		details["sha256"] = sha
	}
	return details
}

func summaryDetailsForAPI(summary dbimport.Summary) map[string]any {
	return map[string]any{
		"inbounds": map[string]any{
			"total":     summary.Inbounds.Total,
			"imported":  summary.Inbounds.Imported,
			"skipped":   summary.Inbounds.Skipped,
			"conflicts": summary.Inbounds.Conflicts,
		},
		"endpoints": map[string]any{
			"imported": summary.Endpoints.Imported,
			"skipped":  summary.Endpoints.Skipped,
		},
		"tls": map[string]any{
			"created": summary.TLS.Created,
			"reused":  summary.TLS.Reused,
		},
		"clients": map[string]any{
			"unique_emails": summary.Clients.UniqueEmails,
			"merged":        summary.Clients.Merged,
			"created":       summary.Clients.Created,
		},
	}
}
