package importxui

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbimport "github.com/MalenkiySolovey/solovey-ui/database/importxui"
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
	strategy := dbimport.Strategy(upload.Fields["strategy"])
	if strategy == "" {
		strategy = dbimport.StrategyMerge
	}
	if err := strategy.Validate(); err != nil {
		a.recordImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	var backupPath string
	if !dryRun {
		var err error
		backupPath, err = dbimport.WritePreImportBackup(time.Now().Unix())
		if err != nil {
			a.recordImportFailure(c, err, upload.SHA256)
			xuiImportError(c, err)
			return
		}
	}
	plan, err := dbimport.Plan(upload.Path, dbimport.PlanOptions{
		Context:   ctx,
		Strategy:  strategy,
		AdminMode: dbimport.AdminModeSkip,
	})
	var report *dbimport.Report
	if err == nil {
		report, err = dbimport.Apply(upload.Path, *plan, dbimport.ApplyOptions{
			Context:    ctx,
			DryRun:     dryRun,
			SkipBackup: true,
			SkipAudit:  true,
			Hostname:   a.Hostname(c),
		})
	}
	if err != nil {
		a.recordImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	report.BackupPath = backupPath
	if !dryRun {
		a.recordImportSuccess(c, report, upload.SHA256)
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
		Hostname:  a.Hostname(c),
		OnProgress: func(progress dbimport.Progress) {
			realtime.Publish(realtime.TopicXUIImportProgress, progress)
		},
	})
	if err != nil {
		a.recordImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	a.recordImportSuccess(c, report, upload.SHA256)
	a.JSONObj(c, report, nil)
}

func (a *Handler) ImportXuiRollback(c *gin.Context) {
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	if !a.enforceRateLimit(c) {
		return
	}
	backupPath := xuiRollbackBackupPath(c)
	if err := validateRollbackPath(backupPath); err != nil {
		a.recordRollbackInvalidBackup(c)
		xuiImportError(c, err)
		return
	}
	// #nosec G304 -- backupPath is constrained to the per-request upload temp directory.
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
	a.recordRollbackSuccess(c, backupPath)
	realtime.Publish(realtime.TopicConfigInvalidated, nil)
	a.JSONMsg(c, "import-xui", nil)
}

func (a *Handler) ImportXuiReports(c *gin.Context) {
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	if !a.enforceRateLimit(c) {
		return
	}
	events, err := a.AuditService.ListXUIImportReports(50)
	a.JSONObj(c, events, err)
}
