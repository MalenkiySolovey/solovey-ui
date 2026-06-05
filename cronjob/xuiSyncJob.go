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

// xuiSyncBackoff is the schedule of sleeps between sync retry attempts.
// Index 0 is sleep after attempt 1, index 1 is sleep after attempt 2.
// Attempt 3 is the final attempt, so no sleep is needed afterwards.
var xuiSyncBackoff = []time.Duration{200 * time.Millisecond, time.Second}

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
			if err := j.recordRun(profile, "success", report); err != nil {
				logger.Warning("xui-sync profile ", profile.Id, ": persist success run failed: ", err)
			}
			return nil
		}
		lastErr = err
		if attempt < 3 {
			delay := xuiSyncBackoff[attempt-1]
			timer := time.NewTimer(delay)
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
	summary := map[string]any{"error": "failed"}
	if lastErr != nil {
		summary["error"] = redact.String(lastErr.Error())
		summary["errorClass"] = classifyXUISyncError(lastErr)
	}
	_ = j.recordRun(profile, "failed", summary)
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
	adminMode := importxui.AdminMode(profile.AdminMode)
	if adminMode == "" {
		adminMode = importxui.AdminModeSkip
	}
	plan, err := importxui.Plan(localPath, importxui.PlanOptions{
		Context:         ctx,
		Strategy:        strategy,
		OnlyNew:         profile.OnlyNew,
		IncludeSettings: profile.IncludeSettings,
		IncludeHistory:  profile.IncludeHistory,
		IncludeRouting:  profile.IncludeRouting,
		AdminMode:       adminMode,
	})
	if err != nil {
		return nil, err
	}
	report, err := importxui.Apply(localPath, *plan, importxui.ApplyOptions{
		Context:   ctx,
		SkipAudit: true,
		OnlyNew:   profile.OnlyNew,
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
	// file/ssh sources cannot be confined by the SSRF/locality guard, so the cron
	// runner executes them only when an admin saved the profile (SourceTrusted).
	// A legacy profile (persisted before trust tracking) or a scoped-token-authored
	// one must be re-saved by an admin before its file/ssh source will run — this
	// closes the non-retroactive gap where a pre-fix scoped-authored file/ssh
	// profile would otherwise still run with full trust.
	if (source.Type == "file" || source.Type == "ssh") && !source.SourceTrusted {
		return nil, fmt.Errorf("file/ssh sync profile must be re-saved by an admin before it can run")
	}
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
		// source.RestrictPrivate is true when an untrusted scoped token authored
		// the profile; re-apply the dial-time SSRF/locality block-list so a
		// save-time-public host re-pointed to loopback/LAN before this run is
		// rejected. Admin-authored profiles keep RestrictPrivate=false so legit
		// same-host/LAN migrations still work.
		return xuihttp.New(baseURL, source.Username, source.Password).WithRestrictPrivate(source.RestrictPrivate), nil
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
