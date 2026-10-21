//go:build !minimal

package importxui

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	dbimport "github.com/MalenkiySolovey/solovey-ui/components/import-xui/database"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) recordImportFailure(c *gin.Context, err error, sha string) {
	details := map[string]any{
		"reason":      xuiImportErrorClass(err),
		"operationId": c.GetString(importOperationKey),
	}
	if sha != "" {
		details["sha256"] = sha
	}
	if errors.Is(err, dbimport.ErrBusy) {
		a.Audit(c, a.Actor(c), "panel_import_busy", "database", service.AuditSeverityWarn, details)
		return
	}
	a.Audit(c, a.Actor(c), "panel_import_failed", "database", service.AuditSeverityWarn, details)
}

func (a *Handler) recordRollbackInvalidBackup(c *gin.Context) {
	a.Audit(c, a.Actor(c), "panel_import_failed", "database", service.AuditSeverityWarn, map[string]any{"reason": "invalid_backup", "operationId": c.GetString(importOperationKey)})
}

func (a *Handler) recordRollbackSuccess(c *gin.Context, backupPath string) {
	a.Audit(c, a.Actor(c), "panel_import_rollback", "database", service.AuditSeverityWarn, map[string]any{
		"backup":      filepath.Base(backupPath),
		"operationId": c.GetString(importOperationKey),
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
	errorClass := xuiImportErrorClass(err)
	c.JSON(status, Envelope{
		Success: false,
		Msg:     xuiImportSafeMessage(errorClass),
	})
}

func xuiImportSafeMessage(class string) string {
	switch class {
	case "payload_too_large":
		return "Compatible panel import payload is too large"
	case "busy":
		return "Compatible panel import is busy"
	case "plan_stale":
		return "Compatible panel import plan is stale"
	case "plan_invalid":
		return "Compatible panel import plan is invalid"
	case "not_sqlite":
		return "Compatible panel database is invalid"
	default:
		return "Compatible panel import failed"
	}
}

func xuiImportErrorClass(err error) string {
	var maxBytesErr *http.MaxBytesError
	var fieldTooLargeErr *xuiFieldTooLargeError
	switch {
	case errors.As(err, &maxBytesErr), errors.As(err, &fieldTooLargeErr), strings.Contains(err.Error(), "request body too large"):
		return "payload_too_large"
	case errors.Is(err, dbimport.ErrBusy), strings.Contains(err.Error(), "panel_import_busy"):
		return "busy"
	case errors.Is(err, dbimport.ErrPlanStale), strings.Contains(err.Error(), "plan_stale"):
		return "plan_stale"
	case errors.Is(err, dbimport.ErrPlanInvalid), strings.Contains(err.Error(), "plan_invalid"):
		return "plan_invalid"
	case strings.Contains(err.Error(), "not_sqlite"), strings.Contains(strings.ToLower(err.Error()), "sqlite"):
		return "not_sqlite"
	default:
		return "failed"
	}
}
