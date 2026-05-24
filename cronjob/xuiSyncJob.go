package cronjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/importxui"
	xfile "github.com/deposist/s-ui-x/database/importxui/source/file"
	xssh "github.com/deposist/s-ui-x/database/importxui/source/ssh"
	"github.com/deposist/s-ui-x/database/importxui/source/xuihttp"
	"github.com/deposist/s-ui-x/database/model"
	"github.com/deposist/s-ui-x/logger"
	"github.com/deposist/s-ui-x/util/redact"
)

const xuiSyncMinInterval = 10 * time.Minute

type XUISyncJob struct {
	now func() time.Time
}

func NewXUISyncJob() *XUISyncJob {
	return &XUISyncJob{now: time.Now}
}

func (j *XUISyncJob) Run() {
	if err := j.RunAll(context.Background()); err != nil {
		logger.Warning("xui-sync: ", err)
	}
}

func (j *XUISyncJob) RunAll(ctx context.Context) error {
	if os.Getenv("XUI_DISABLE_REMOTE") == "1" {
		return fmt.Errorf("xui remote sync is disabled")
	}
	db := database.GetDB()
	if db == nil {
		return fmt.Errorf("database is not initialized")
	}
	var profiles []model.XUISyncProfile
	if err := db.Where("enabled = ?", true).Find(&profiles).Error; err != nil {
		return err
	}
	for i := range profiles {
		if err := j.RunProfile(ctx, &profiles[i]); err != nil {
			logger.Warning("xui-sync profile ", profiles[i].Id, ": ", err)
		}
	}
	return nil
}

func (j *XUISyncJob) RunProfile(ctx context.Context, profile *model.XUISyncProfile) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if os.Getenv("XUI_DISABLE_REMOTE") == "1" {
		return fmt.Errorf("xui remote sync is disabled")
	}
	now := j.now()
	if profile.LastRunAt > 0 && now.Sub(time.Unix(profile.LastRunAt, 0)) < xuiSyncMinInterval {
		return fmt.Errorf("xui sync profile %d was run too recently", profile.Id)
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		report, err := j.runProfileOnce(ctx, profile)
		if err == nil {
			return j.recordRun(profile, "success", report)
		}
		lastErr = err
		if attempt < 3 {
			timer := time.NewTimer(time.Duration(attempt) * 100 * time.Millisecond)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	_ = j.recordRun(profile, "failed", map[string]any{"error": "failed"})
	recordSyncAudit("xui_sync_failed", profile, nil, lastErr)
	return lastErr
}

func (j *XUISyncJob) runProfileOnce(ctx context.Context, profile *model.XUISyncProfile) (*importxui.Report, error) {
	source, err := importxui.LoadSyncProfileSource(*profile)
	if err != nil {
		return nil, err
	}
	src, err := sourceFromProfile(source)
	if err != nil {
		return nil, err
	}
	localPath, cleanup, err := src.Acquire(ctx)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, err
	}
	strategy := importxui.Strategy(profile.Strategy)
	if strategy == "" {
		strategy = importxui.StrategyMerge
	}
	plan, err := importxui.Plan(localPath, importxui.PlanOptions{
		Context:  ctx,
		Strategy: strategy,
		OnlyNew:  true,
	})
	if err != nil {
		return nil, err
	}
	report, err := importxui.Apply(localPath, *plan, importxui.ApplyOptions{
		Context:   ctx,
		SkipAudit: true,
		OnlyNew:   true,
	})
	if err != nil {
		return nil, err
	}
	recordSyncAudit("xui_sync_run", profile, report)
	return report, nil
}

func (j *XUISyncJob) recordRun(profile *model.XUISyncProfile, status string, summary any) error {
	db := database.GetDB()
	if db == nil {
		return fmt.Errorf("database is not initialized")
	}
	return importxui.UpdateSyncProfileRun(db, profile, status, summary, j.now().Unix())
}

func sourceFromProfile(source importxui.SyncProfileSource) (importxui.Source, error) {
	switch source.Type {
	case "file":
		return xfile.New(source.URL), nil
	case "ssh":
		cfg := xssh.Source{
			Addr:               xssh.Addr(source.Host, source.Port),
			User:               source.Username,
			Password:           source.Password,
			KeyPath:            source.KeyPath,
			RemotePath:         source.RemotePath,
			ConfirmHostKey:     source.ConfirmHostKey,
			HostKeyFingerprint: source.HostKeyFingerprint,
		}
		if cfg.RemotePath == "" {
			cfg.RemotePath = "/etc/x-ui/x-ui.db"
		}
		if source.URL != "" {
			parsed, err := xssh.New(source.URL)
			if err != nil {
				return nil, err
			}
			if cfg.User == "" {
				cfg.User = parsed.User
			}
			if cfg.Password == "" {
				cfg.Password = parsed.Password
			}
			if cfg.RemotePath == "" {
				cfg.RemotePath = parsed.RemotePath
			}
			if cfg.Addr == ":22" {
				cfg.Addr = parsed.Addr
			}
		}
		return cfg, nil
	case "xuihttp":
		baseURL := source.BaseURL
		if baseURL == "" {
			baseURL = source.URL
		}
		return xuihttp.New(baseURL, source.Username, source.Password), nil
	default:
		return nil, fmt.Errorf("unsupported xui sync source type %q", source.Type)
	}
}

func recordSyncAudit(event string, profile *model.XUISyncProfile, report *importxui.Report, syncErr ...error) {
	db := database.GetDB()
	if db == nil {
		return
	}
	details := map[string]any{
		"profile_id":  profile.Id,
		"source_type": profile.SourceType,
	}
	if report != nil {
		details["summary"] = report.Summary
	}
	if len(syncErr) > 0 && syncErr[0] != nil {
		details["errorClass"] = classifyXUISyncError(syncErr[0])
	}
	raw, _ := json.Marshal(details)
	_ = db.Create(&model.AuditEvent{
		DateTime: time.Now().Unix(),
		Actor:    "system",
		Event:    event,
		Resource: "database",
		Severity: "info",
		Details:  raw,
	}).Error
}

func classifyXUISyncError(err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled"
	}
	message := strings.ToLower(redact.String(err.Error()))
	switch {
	case strings.Contains(message, "disabled"):
		return "disabled"
	case strings.Contains(message, "database"),
		strings.Contains(message, "sqlite"),
		strings.Contains(message, "constraint"),
		strings.Contains(message, "no such table"):
		return "db"
	case strings.Contains(message, "source"),
		strings.Contains(message, "ssh"),
		strings.Contains(message, "http"),
		strings.Contains(message, "no such file"),
		strings.Contains(message, "cannot find"),
		strings.Contains(message, "unsupported xui sync source"):
		return "source"
	default:
		return "failed"
	}
}
