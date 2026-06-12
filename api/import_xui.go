package api

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/importxui"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/realtime"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) ImportXui(c *gin.Context) {
	ctx, cancel, ok := a.beginXUIRequest(c)
	if !ok {
		return
	}
	defer cancel()
	extendSlowRequestDeadlines(c)
	upload, err := saveXUIUpload(c)
	if err != nil {
		a.recordXuiImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	defer os.RemoveAll(upload.Dir)

	dryRun := upload.Fields["dryRun"] == "1"
	strategy := importxui.Strategy(upload.Fields["strategy"])
	if strategy == "" {
		strategy = importxui.StrategyMerge
	}
	if err := strategy.Validate(); err != nil {
		a.recordXuiImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	var backupPath string
	if !dryRun {
		var err error
		backupPath, err = importxui.WritePreImportBackup(time.Now().Unix())
		if err != nil {
			a.recordXuiImportFailure(c, err, upload.SHA256)
			xuiImportError(c, err)
			return
		}
	}
	report, err := importxui.Import(upload.Path, importxui.Options{
		Context:   ctx,
		DryRun:    dryRun,
		Strategy:  strategy,
		SkipAudit: true,
		Hostname:  getHostname(c),
	})
	if err != nil {
		a.recordXuiImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	report.BackupPath = backupPath
	if !dryRun {
		a.recordXuiImportSuccess(c, report, upload.SHA256)
	}
	jsonObj(c, report, nil)
}

func (a *ApiService) ImportXuiPlan(c *gin.Context) {
	ctx, cancel, ok := a.beginXUIRequest(c)
	if !ok {
		return
	}
	defer cancel()
	extendSlowRequestDeadlines(c)
	upload, err := saveXUIUpload(c)
	if err != nil {
		a.recordXuiImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	defer os.RemoveAll(upload.Dir)

	strategy := importxui.Strategy(upload.Fields["strategy"])
	if strategy == "" {
		strategy = importxui.StrategyMerge
	}
	adminMode := importxui.AdminMode(upload.Fields["adminMode"])
	if adminMode == "" {
		adminMode = importxui.AdminModeSkip
	}
	plan, err := importxui.Plan(upload.Path, importxui.PlanOptions{
		Context:         ctx,
		Strategy:        strategy,
		IncludeSettings: upload.Fields["includeSettings"] == "1",
		IncludeHistory:  upload.Fields["includeHistory"] == "1",
		IncludeRouting:  upload.Fields["includeRouting"] == "1",
		AdminMode:       adminMode,
	})
	if err != nil {
		a.recordXuiImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	plan.Source.Path = ""
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(http.StatusOK)
	_ = json.NewEncoder(c.Writer).Encode(Msg{Success: true, Obj: plan})
}

func (a *ApiService) ImportXuiApply(c *gin.Context) {
	ctx, cancel, ok := a.beginXUIRequest(c)
	if !ok {
		return
	}
	defer cancel()
	extendSlowRequestDeadlines(c)
	upload, err := saveXUIUpload(c)
	if err != nil {
		a.recordXuiImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	defer os.RemoveAll(upload.Dir)

	plan, err := decodeXUIApplyPlan(upload)
	if err != nil {
		a.recordXuiImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	report, err := importxui.Apply(upload.Path, plan, importxui.ApplyOptions{
		Context:   ctx,
		SkipAudit: true,
		Hostname:  getHostname(c),
		OnProgress: func(progress importxui.Progress) {
			realtime.Publish(realtime.TopicXUIImportProgress, progress)
		},
	})
	if err != nil {
		a.recordXuiImportFailure(c, err, upload.SHA256)
		xuiImportError(c, err)
		return
	}
	a.recordXuiImportSuccess(c, report, upload.SHA256)
	jsonObj(c, report, nil)
}

func (a *ApiService) ImportXuiRollback(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "database", "admin") {
		return
	}
	if !a.enforceXUIRateLimit(c) {
		return
	}
	backupPath := xuiRollbackBackupPath(c)
	if err := validateRollbackPath(backupPath); err != nil {
		a.recordXuiRollbackInvalidBackup(c)
		xuiImportError(c, err)
		return
	}
	// #nosec G304 -- backupPath is constrained to the per-request upload temp directory.
	file, err := os.Open(backupPath)
	if err != nil {
		a.recordXuiImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	defer file.Close()
	if err := database.ImportDB(multipart.File(file)); err != nil {
		a.recordXuiImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	a.recordXuiRollbackSuccess(c, backupPath)
	realtime.Publish(realtime.TopicConfigInvalidated, nil)
	jsonMsg(c, "import-xui", nil)
}

func (a *ApiService) ImportXuiReports(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "database", "admin") {
		return
	}
	if !a.enforceXUIRateLimit(c) {
		return
	}
	var events []model.AuditEvent
	err := database.GetDB().
		Where("event IN ?", []string{"xui_import", "xui_import_failed", "xui_import_rollback"}).
		Order("date_time desc").
		Limit(50).
		Find(&events).Error
	jsonObj(c, events, err)
}
