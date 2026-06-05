package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/deposist/s-ui-x/cronjob"
	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/importxui"
	xfile "github.com/deposist/s-ui-x/database/importxui/source/file"
	xssh "github.com/deposist/s-ui-x/database/importxui/source/ssh"
	"github.com/deposist/s-ui-x/database/importxui/source/xuihttp"
	"github.com/deposist/s-ui-x/database/model"
	"github.com/deposist/s-ui-x/realtime"
	"github.com/deposist/s-ui-x/service"
	"github.com/deposist/s-ui-x/util/ssrf"

	"github.com/gin-gonic/gin"
)

type xuiRemoteRequest struct {
	Source          importxui.SyncProfileSource `json:"source"`
	Strategy        string                      `json:"strategy"`
	IncludeSettings bool                        `json:"includeSettings"`
	IncludeHistory  bool                        `json:"includeHistory"`
	IncludeRouting  bool                        `json:"includeRouting"`
	AdminMode       string                      `json:"adminMode"`
	Plan            importxui.MigrationPlan     `json:"plan"`
}

func (a *ApiService) ImportXuiRemotePlan(c *gin.Context) {
	ctx, cancel, ok := a.beginXUIRemoteRequest(c)
	if !ok {
		return
	}
	defer cancel()
	var req xuiRemoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		xuiImportError(c, err)
		return
	}
	src, err := apiSourceFromConfig(req.Source, a.remoteImportIsUntrusted(c))
	if err != nil {
		xuiImportError(c, err)
		return
	}
	plan, err := importxui.PlanFromSource(src, importxui.PlanOptions{
		Context:         ctx,
		Strategy:        importxui.Strategy(firstNonEmptyString(req.Strategy, string(importxui.StrategyMerge))),
		IncludeSettings: req.IncludeSettings,
		IncludeHistory:  req.IncludeHistory,
		IncludeRouting:  req.IncludeRouting,
		AdminMode:       importxui.AdminMode(firstNonEmptyString(req.AdminMode, string(importxui.AdminModeSkip))),
	})
	if err != nil {
		a.recordXuiImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	plan.Source.Path = ""
	jsonObj(c, plan, nil)
}

func (a *ApiService) ImportXuiRemoteApply(c *gin.Context) {
	ctx, cancel, ok := a.beginXUIRemoteRequest(c)
	if !ok {
		return
	}
	defer cancel()
	var req xuiRemoteRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		xuiImportError(c, err)
		return
	}
	src, err := apiSourceFromConfig(req.Source, a.remoteImportIsUntrusted(c))
	if err != nil {
		xuiImportError(c, err)
		return
	}
	report, err := importxui.ApplyFromSource(src, req.Plan, importxui.ApplyOptions{
		Context:   ctx,
		SkipAudit: true,
		Hostname:  getHostname(c),
		OnProgress: func(progress importxui.Progress) {
			realtime.Publish(realtime.TopicXUIImportProgress, progress)
		},
	})
	if err != nil {
		a.recordXuiImportFailure(c, err, "")
		xuiImportError(c, err)
		return
	}
	a.recordAudit(c, requestActor(c), "xui_import", "database", service.AuditSeverityInfo, remoteImportAuditDetails(req.Source, report, req.Plan.Source.Hash))
	jsonObj(c, report, nil)
}

func (a *ApiService) XUISyncProfiles(c *gin.Context) {
	if !a.requireXUIRemoteScope(c) {
		return
	}
	if !a.enforceXUIRateLimit(c) {
		return
	}
	var profiles []model.XUISyncProfile
	err := database.GetDB().Order("id desc").Find(&profiles).Error
	jsonObj(c, profiles, err)
}

func (a *ApiService) SaveXUISyncProfile(c *gin.Context) {
	_, cancel, ok := a.beginXUIRemoteRequest(c)
	if !ok {
		return
	}
	defer cancel()
	var input importxui.SyncProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		xuiImportError(c, err)
		return
	}
	untrusted := a.remoteImportIsUntrusted(c)
	if untrusted {
		if err := validateRemoteSyncSourceSSRF(c.Request.Context(), input.Source); err != nil {
			xuiImportError(c, err)
			return
		}
	}
	// Force the trust level from the auth context (never from client input) and
	// persist it inside the encrypted source. RestrictPrivate makes the cron
	// runner re-apply the SSRF/locality block-list to an untrusted-authored http
	// profile (closing the save/run DNS-rebind TOCTOU); SourceTrusted is the
	// positive admin-authored marker the cron runner requires before executing a
	// file/ssh source, so legacy/untrusted file/ssh profiles are not run trusted.
	input.Source.RestrictPrivate = untrusted
	input.Source.SourceTrusted = !untrusted
	profile, err := importxui.SaveSyncProfile(input)
	if err == nil {
		a.recordAudit(c, requestActor(c), "xui_sync_profile_save", "database", service.AuditSeverityInfo, map[string]any{
			"profile_id":  profile.Id,
			"source_type": profile.SourceType,
		})
	}
	jsonObj(c, profile, err)
}

func (a *ApiService) RunXUISyncProfile(c *gin.Context) {
	ctx, cancel, ok := a.beginXUIRemoteRequest(c)
	if !ok {
		return
	}
	defer cancel()
	id, err := strconv.ParseUint(firstNonEmptyString(c.PostForm("id"), c.Query("id")), 10, 64)
	if err != nil || id == 0 {
		xuiImportError(c, fmt.Errorf("invalid profile id"))
		return
	}
	var profile model.XUISyncProfile
	if err := database.GetDB().First(&profile, id).Error; err != nil {
		xuiImportError(c, err)
		return
	}
	err = cronjob.NewXUISyncJob().RunProfile(ctx, &profile)
	jsonObj(c, profile, err)
}

func (a *ApiService) DisableXUISyncProfile(c *gin.Context) {
	if !a.requireXUIRemoteScope(c) {
		return
	}
	if !a.enforceXUIRateLimit(c) {
		return
	}
	id, err := strconv.ParseUint(firstNonEmptyString(c.PostForm("id"), c.Query("id")), 10, 64)
	if err != nil || id == 0 {
		xuiImportError(c, fmt.Errorf("invalid profile id"))
		return
	}
	err = database.GetDB().Model(&model.XUISyncProfile{}).Where("id = ?", id).Update("enabled", false).Error
	jsonMsg(c, "xui-sync-disable", err)
}

func (a *ApiService) XUIRemoteStatus(c *gin.Context) {
	if !a.requireXUIRemoteScope(c) {
		return
	}
	jsonObj(c, gin.H{"disabled": os.Getenv("XUI_DISABLE_REMOTE") == "1"}, nil)
}

func (a *ApiService) requireXUIRemoteScope(c *gin.Context) bool {
	return a.requireTokenScopeAny(c, "xui_remote", "xui_remote")
}

func (a *ApiService) beginXUIRemoteRequest(c *gin.Context) (context.Context, context.CancelFunc, bool) {
	if !a.requireXUIRemoteScope(c) {
		return c.Request.Context(), func() {}, false
	}
	if !remoteEnabled(c) {
		a.recordAudit(c, requestActor(c), "xui_import_failed", "xui_remote", service.AuditSeverityWarn, map[string]any{
			"reason": "remote_disabled",
		})
		return c.Request.Context(), func() {}, false
	}
	if !a.enforceXUIRateLimit(c) {
		return c.Request.Context(), func() {}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxXUIImportBytes)
	ctx, cancel := context.WithTimeout(c.Request.Context(), xuiRequestTimeout)
	c.Request = c.Request.WithContext(ctx)
	return ctx, cancel, true
}

func remoteEnabled(c *gin.Context) bool {
	if os.Getenv("XUI_DISABLE_REMOTE") != "1" {
		return true
	}
	c.JSON(http.StatusForbidden, Msg{Success: false, Msg: "xui remote disabled"})
	return false
}

// remoteImportIsUntrusted reports whether the caller is a scoped API token
// rather than a full admin session. Only such callers are restricted from
// reaching loopback/private hosts via the remote x-ui importer (S1 SSRF guard);
// admin sessions, admin-scoped tokens, the CLI and cron stay unrestricted so
// same-host and LAN migrations keep working. Infrastructure/cloud-metadata
// targets are blocked for everyone in the importer's guarded client.
func (a *ApiService) remoteImportIsUntrusted(c *gin.Context) bool {
	scope, hasScope := requestTokenScope(c)
	return hasScope && scope != "admin"
}

// sourceIsLocalOrSSH reports whether an import source reads a local file or makes
// an outbound SSH connection. Neither can be confined by the SSRF/locality guard
// (which only constrains outbound HTTP to the remote panel), so both are
// restricted to trusted (admin) callers.
func sourceIsLocalOrSSH(source importxui.SyncProfileSource) bool {
	switch source.Type {
	case "file", "ssh":
		return true
	case "xuihttp":
		return false
	default:
		return !strings.HasPrefix(source.URL, "http://") && !strings.HasPrefix(source.URL, "https://")
	}
}

// validateRemoteSyncSourceSSRF is the save-time guard applied to an untrusted
// (scoped, non-admin) caller. file/ssh sources are rejected outright: the trusted
// cron job would otherwise execute a stored profile with no locality confinement
// (arbitrary local SQLite read / outbound SSH with attacker creds). http(s)
// sources are validated against the SSRF block-list so a private/loopback/
// metadata target cannot be persisted.
func validateRemoteSyncSourceSSRF(ctx context.Context, source importxui.SyncProfileSource) error {
	if sourceIsLocalOrSSH(source) {
		return fmt.Errorf("file and ssh sources require an admin session, not a scoped token")
	}
	baseURL := firstNonEmptyString(source.BaseURL, source.URL)
	if baseURL == "" {
		return fmt.Errorf("missing source url")
	}
	return ssrf.ValidateOutboundURL(ctx, baseURL, "http", "https")
}

func apiSourceFromConfig(source importxui.SyncProfileSource, restrictPrivate bool) (importxui.Source, error) {
	// An untrusted (scoped) caller may only use an SSRF-confinable http(s) source.
	// file/ssh sources are admin-only; without this guard the file branch below
	// silently dropped restrictPrivate and read any local SQLite path.
	if restrictPrivate && sourceIsLocalOrSSH(source) {
		return nil, fmt.Errorf("file and ssh sources require an admin session, not a scoped token")
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
		if source.URL != "" {
			parsed, err := xssh.New(source.URL)
			if err != nil {
				return nil, err
			}
			if cfg.Addr == ":22" {
				cfg.Addr = parsed.Addr
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
		}
		return cfg, nil
	case "xuihttp":
		baseURL := firstNonEmptyString(source.BaseURL, source.URL)
		return xuihttp.New(baseURL, source.Username, source.Password).WithRestrictPrivate(restrictPrivate), nil
	default:
		if strings.HasPrefix(source.URL, "ssh://") {
			parsed, err := xssh.New(source.URL)
			if err != nil {
				return nil, err
			}
			parsed.ConfirmHostKey = source.ConfirmHostKey
			parsed.HostKeyFingerprint = source.HostKeyFingerprint
			return parsed, nil
		}
		if strings.HasPrefix(source.URL, "http://") || strings.HasPrefix(source.URL, "https://") {
			return xuihttp.New(source.URL, source.Username, source.Password).WithRestrictPrivate(restrictPrivate), nil
		}
		return nil, fmt.Errorf("unsupported xui source type")
	}
}

func remoteImportAuditDetails(source importxui.SyncProfileSource, report *importxui.Report, hash string) map[string]any {
	details := summaryDetailsForAPI(report.Summary)
	details["source_type"] = firstNonEmptyString(source.Type, syncSourceType(source))
	if hash != "" {
		details["sha256"] = hash
	}
	if host := syncSafeHostPort(source); host != "" {
		details["host"] = host
	}
	return details
}

func syncSourceType(source importxui.SyncProfileSource) string {
	if strings.HasPrefix(source.URL, "ssh://") {
		return "ssh"
	}
	if strings.HasPrefix(source.URL, "http://") || strings.HasPrefix(source.URL, "https://") {
		return "xuihttp"
	}
	return source.Type
}

func syncSafeHostPort(source importxui.SyncProfileSource) string {
	if source.Host != "" {
		if source.Port > 0 {
			return net.JoinHostPort(source.Host, strconv.Itoa(source.Port))
		}
		return source.Host
	}
	if source.URL == "" {
		return ""
	}
	parsed, err := url.Parse(source.URL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
