//go:build !minimal

package importxui

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"

	dbimport "github.com/MalenkiySolovey/solovey-ui/components/import-xui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/realtime"

	"github.com/gin-gonic/gin"
)

func (a *Handler) ImportXui(c *gin.Context) {
	ctx, cancel, ok := a.beginRequest(c)
	if !ok {
		return
	}
	defer cancel()
	extendSlowRequestDeadlines(c)
	upload, err := saveUpload(c)
	if err != nil {
		a.recordImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	defer os.RemoveAll(upload.Dir)

	dryRun := upload.Fields["dryRun"] == "1"
	if !dryRun {
		if !a.requireMutationStepUp(c) || !a.requireMutationDependencies(c) {
			return
		}
	}
	strategy := dbimport.Strategy(upload.Fields["strategy"])
	if strategy == "" {
		strategy = dbimport.StrategyMerge
	}
	if err := strategy.Validate(); err != nil {
		a.recordImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	plan, err := dbimport.Plan(upload.Path, dbimport.PlanOptions{
		Context:   ctx,
		Strategy:  strategy,
		AdminMode: dbimport.AdminModeSkip,
	})
	var report *dbimport.Report
	if err == nil {
		report, err = dbimport.Apply(upload.Path, *plan, dbimport.ApplyOptions{
			Context:   ctx,
			DryRun:    dryRun,
			SkipAudit: true,
			Hostname:  a.hostname(c),
		})
	}
	if err != nil {
		a.recordImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	if !dryRun {
		a.recordImportSuccess(c, report, upload.SHA256)
		a.ConfigChanged()
	}
	a.JSONObj(c, report, nil)
}

func (a *Handler) ImportXuiPlan(c *gin.Context) {
	ctx, cancel, ok := a.beginRequest(c)
	if !ok {
		return
	}
	defer cancel()
	extendSlowRequestDeadlines(c)
	upload, err := saveUpload(c)
	if err != nil {
		a.recordImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	defer os.RemoveAll(upload.Dir)

	strategy := dbimport.Strategy(upload.Fields["strategy"])
	if strategy == "" {
		strategy = dbimport.StrategyMerge
	}
	adminMode := dbimport.AdminMode(upload.Fields["adminMode"])
	if adminMode == "" {
		adminMode = dbimport.AdminModeSkip
	}
	plan, err := dbimport.Plan(upload.Path, dbimport.PlanOptions{
		Context:         ctx,
		Strategy:        strategy,
		IncludeSettings: upload.Fields["includeSettings"] == "1",
		IncludeHistory:  upload.Fields["includeHistory"] == "1",
		IncludeRouting:  upload.Fields["includeRouting"] == "1",
		AdminMode:       adminMode,
	})
	if err != nil {
		a.recordImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	plan.Source.Path = ""
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(http.StatusOK)
	_ = json.NewEncoder(c.Writer).Encode(Envelope{Success: true, Obj: plan})
}

func (a *Handler) ImportXuiApply(c *gin.Context) {
	ctx, cancel, ok := a.beginRequest(c)
	if !ok {
		return
	}
	defer cancel()
	if !a.requireMutationStepUp(c) {
		return
	}
	if !a.requireMutationDependencies(c) {
		return
	}
	extendSlowRequestDeadlines(c)
	upload, err := saveUpload(c)
	if err != nil {
		a.recordImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	defer os.RemoveAll(upload.Dir)

	plan, err := decodeApplyPlan(upload)
	if err != nil {
		a.recordImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	report, err := dbimport.Apply(upload.Path, plan, dbimport.ApplyOptions{
		Context:   ctx,
		SkipAudit: true,
		Hostname:  a.hostname(c),
		OnProgress: func(progress dbimport.Progress) {
			realtime.Publish(realtime.TopicComponentProgress, realtime.ComponentProgress{
				ComponentID: "import-xui",
				Progress:    progress,
			})
		},
	})
	if err != nil {
		a.recordImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	a.recordImportSuccess(c, report, upload.SHA256)
	a.ConfigChanged()
	a.JSONObj(c, report, nil)
}

func (a *Handler) ImportXuiRollback(c *gin.Context) {
	if !a.requireBaseDependencies(c, false) || a.JSONMsg == nil {
		if !c.IsAborted() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, Envelope{Success: false, Msg: "Compatible panel import is unavailable"})
		}
		return
	}
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	if !a.enforceRateLimit(c) {
		return
	}
	if !a.requireMutationStepUp(c) {
		return
	}
	backupReference := xuiRollbackBackupPath(c)
	backupPath, err := resolveRollbackPath(backupReference)
	if err != nil {
		a.recordRollbackInvalidBackup(c)
		xuiImportError(c, err)
		return
	}
	// #nosec G304 -- backupPath is resolved from a basename-only reference
	// under the configured database directory and rejects symlinks.
	file, err := os.Open(backupPath)
	if err != nil {
		a.recordImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	defer file.Close()
	if err := backup.Restore(multipart.File(file)); err != nil {
		a.recordImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	a.recordRollbackSuccess(c, backupReference)
	realtime.Publish(realtime.TopicConfigInvalidated, nil)
	a.JSONMsg(c, "import-xui", nil)
}

func (a *Handler) ImportXuiReports(c *gin.Context) {
	if !a.requireBaseDependencies(c, true) || a.AuditHistory == nil {
		if !c.IsAborted() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, Envelope{Success: false, Msg: "Compatible panel import is unavailable"})
		}
		return
	}
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	if !a.enforceRateLimit(c) {
		return
	}
	events, err := a.AuditHistory.ListByEvents(50, []string{"panel_import", "panel_import_failed", "panel_import_rollback"})
	a.JSONObj(c, events, err)
}
