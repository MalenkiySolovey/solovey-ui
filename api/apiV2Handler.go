package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	authhttp "github.com/MalenkiySolovey/solovey-ui/api/auth"
	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
	dbtransferhttp "github.com/MalenkiySolovey/solovey-ui/api/dbtransfer"
	importxuihttp "github.com/MalenkiySolovey/solovey-ui/api/importxui"
	remotesubhttp "github.com/MalenkiySolovey/solovey-ui/api/remotesub"
	telegramhttp "github.com/MalenkiySolovey/solovey-ui/api/telegram"
	telemetryhttp "github.com/MalenkiySolovey/solovey-ui/api/telemetry"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

type TokenInMemory struct {
	ID          uint   `json:"id"`
	TokenHash   string `json:"tokenHash"`
	TokenPrefix string `json:"tokenPrefix"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
	Expiry      int64  `json:"expiry"`
	Username    string `json:"username"`
}

type APIv2Handler struct {
	ApiService
	auth      *authhttp.Handler
	config    *confighttp.Handler
	db        *dbtransferhttp.Handler
	telemetry *telemetryhttp.Handler
	tokensMu  sync.RWMutex
	tokens    map[string]TokenInMemory
}

const (
	apiUsernameKey              = "apiUsername"
	apiTokenScopeKey            = "apiTokenScope"
	legacyTokenHeaderExpiredKey = "legacyTokenHeaderExpired"
	// #nosec G101 -- a sunset date string, not a credential.
	legacyTokenHeaderSunset = "Sat, 15 Aug 2026 00:00:00 GMT"
)

var (
	apiTokenNow               = time.Now
	legacyTokenHeaderSunsetAt = time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
)

func NewAPIv2Handler(g *gin.RouterGroup, options ...Option) *APIv2Handler {
	a := &APIv2Handler{
		ApiService: NewApiService(options...),
		tokens:     map[string]TokenInMemory{},
	}
	a.auth = a.authHandler()
	a.config = a.configHandler()
	a.db = a.dbTransferHandler()
	a.telemetry = a.telemetryHandler()
	a.ReloadTokens()
	a.initRouter(g)
	return a
}

func (a *APIv2Handler) initRouter(g *gin.RouterGroup) {
	g.Use(func(c *gin.Context) {
		a.checkToken(c)
	})
	g.GET("/security/audit", a.telemetry.GetSecurityAudit)
	g.POST("/rotateSubSecret", a.config.RotateSubSecret)
	telegramhttp.RegisterRoutes(g, telegramhttp.Deps{
		Settings:       a.SettingService,
		Telegram:       a.TelegramService,
		AuditService:   a.AuditService,
		RequireScope:   a.requireTokenScopeAny,
		Actor:          requestActor,
		RemoteIP:       getRemoteIp,
		CheckRateLimit: checkTelegramBackupManualRateLimit,
		Audit:          a.recordAudit,
		JSONObj:        jsonObj,
	})
	g.GET("/logs/entries", a.telemetry.GetLogEntries)
	g.GET("/diagnostics/report", a.telemetry.GetDiagnosticsReport)
	g.GET("/diagnostics/bundle", a.telemetry.GetDiagnosticsBundle)
	importxuihttp.RegisterRoutes(g, a.importXUIDeps())
	remotesubhttp.RegisterRoutes(g, remotesubhttp.Deps{
		Service:        &a.RemoteOutboundService,
		RequireScope:   a.requireTokenScopeAny,
		Actor:          requestActor,
		ValidateTarget: confighttp.ValidateOutboundCheckTarget,
		JSONObj:        jsonObj,
		JSONMsg:        jsonMsg,
	})
	g.POST("/:postAction", a.postHandler)
	g.GET("/:getAction", a.getHandler)
}

// apiV2ActionScopes maps each apiv2 dispatcher action to the API-token scopes
// permitted to invoke it. "admin" is always allowed (added in enforceActionScope)
// and is also the default token scope, so this only constrains tokens an admin
// deliberately narrowed. Actions that enforce their own scope inside the handler
// (getdb, importdb, rotateSubSecret) are intentionally omitted, as are the
// separately-registered routes (telegram/*, import-xui/*, security/audit).
// Browser sessions carry no token scope and are allowed through by
// requireTokenScopeAny.
var apiV2ActionScopes = map[string][]string{
	// State mutations and active probes require write.
	"save":          {"write"},
	"reorder":       {"write"},
	"restartApp":    {"write"},
	"restartSb":     {"write"},
	"checkOutbound": {"write"},
	"linkConvert":   {"read", "write"},
	"subConvert":    {"read", "write"},
	// Config / identity / secret reads — observability and telegram excluded.
	"load":      {"read", "write"},
	"inbounds":  {"read", "write"},
	"outbounds": {"read", "write"},
	"endpoints": {"read", "write"},
	"services":  {"read", "write"},
	"tls":       {"read", "write"},
	"clients":   {"read", "write"},
	"config":    {"read", "write"},
	"users":     {"read", "write"},
	"settings":  {"read", "write"},
	"changes":   {"read", "write"},
	"keypairs":  {"read", "write"},
	// Operational metrics — observability tokens may read these.
	"stats":   {"read", "write", "observability"},
	"status":  {"read", "write", "observability"},
	"onlines": {"read", "write", "observability"},
	"logs":    {"read", "write", "observability"},
}

// enforceActionScope applies the per-action scope policy for the apiv2 action
// dispatchers. Actions absent from the policy map are allowed through (they
// either self-gate inside the handler or are intentionally open), as are browser
// sessions that carry no token scope. On denial it writes a 403 and returns false.
func (a *APIv2Handler) enforceActionScope(c *gin.Context, action string) bool {
	allowed, ok := apiV2ActionScopes[action]
	if !ok {
		return true
	}
	return a.ApiService.requireTokenScopeAny(c, action, append([]string{"admin"}, allowed...)...)
}

func (a *APIv2Handler) postHandler(c *gin.Context) {
	username := c.GetString(apiUsernameKey)
	action := c.Param("postAction")
	if !a.enforceActionScope(c, action) {
		return
	}

	switch action {
	case "save":
		a.config.Save(c, username)
	case "reorder":
		a.config.Reorder(c, username)
	case "restartApp":
		a.config.RestartApp(c)
	case "restartSb":
		a.config.RestartSb(c)
	case "linkConvert":
		a.config.LinkConvert(c)
	case "subConvert":
		a.config.SubConvert(c)
	case "importdb":
		a.db.ImportDb(c)
	case "rotateSubSecret":
		a.config.RotateSubSecret(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}

func (a *APIv2Handler) getHandler(c *gin.Context) {
	action := c.Param("getAction")
	if !a.enforceActionScope(c, action) {
		return
	}

	switch action {
	case "load":
		a.config.LoadData(c)
	case "inbounds", "outbounds", "endpoints", "services", "tls", "clients", "config":
		err := a.config.LoadPartialData(c, []string{action})
		if err != nil {
			jsonMsg(c, action, err)
		}
		return
	case "users":
		a.auth.GetUsers(c)
	case "settings":
		a.config.GetSettings(c)
	case "stats":
		a.telemetry.GetStats(c)
	case "status":
		a.telemetry.GetStatus(c)
	case "onlines":
		a.telemetry.GetOnlines(c)
	case "logs":
		a.telemetry.GetLogs(c)
	case "changes":
		a.config.CheckChanges(c)
	case "keypairs":
		a.telemetry.GetKeypairs(c)
	case "getdb":
		a.db.DownloadDatabase(c)
	case "checkOutbound":
		a.config.GetCheckOutbound(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}

func (a *APIv2Handler) findUsername(c *gin.Context) string {
	token, legacyHeader := apiTokenFromRequest(c)
	if token == "" {
		return ""
	}
	tokenHash, err := a.UserService.HashAPIToken(token)
	if err != nil {
		logger.Warning("unable to hash API token:", err)
		return ""
	}
	now := time.Now().Unix()
	a.tokensMu.RLock()
	defer a.tokensMu.RUnlock()
	t, ok := a.tokens[tokenHash]
	if !ok {
		return ""
	}
	if !t.Enabled {
		return ""
	}
	if t.Expiry > 0 && t.Expiry < now {
		return ""
	}
	if legacyHeader {
		c.Header("Deprecation", "true")
		c.Header("Sunset", legacyTokenHeaderSunset)
		a.recordAudit(c, t.Username, "legacy_token_header_used", "api_token", service.AuditSeverityWarn, map[string]any{
			"tokenPrefix": t.TokenPrefix,
			"sunset":      legacyTokenHeaderSunset,
		})
	}
	_ = a.UserService.RecordTokenUse(t.ID, getRemoteIp(c))
	c.Set(apiTokenScopeKey, t.Scope)
	return t.Username
}

func (a *APIv2Handler) checkToken(c *gin.Context) {
	username := a.findUsername(c)
	if username != "" {
		c.Set(apiUsernameKey, username)
		c.Next()
		return
	}
	if c.GetBool(legacyTokenHeaderExpiredKey) {
		c.Header("Deprecation", "true")
		c.Header("Sunset", legacyTokenHeaderSunset)
		c.JSON(http.StatusUnauthorized, Msg{
			Success: false,
			Msg:     "legacy token header expired",
		})
		c.Abort()
		return
	}
	c.JSON(http.StatusUnauthorized, Msg{
		Success: false,
		Msg:     "invalid token",
	})
	c.Abort()
}

func (a *APIv2Handler) ReloadTokens() {
	tokens, err := a.auth.LoadTokens()
	if err != nil {
		logger.Error("unable to load tokens: ", err)
		return
	}
	var loaded []TokenInMemory
	if len(tokens) > 0 {
		if err := json.Unmarshal(tokens, &loaded); err != nil {
			logger.Error("unable to load tokens: ", err)
			return
		}
	}
	newMap := make(map[string]TokenInMemory, len(loaded))
	for _, t := range loaded {
		newMap[t.TokenHash] = t
	}
	a.tokensMu.Lock()
	a.tokens = newMap
	a.tokensMu.Unlock()
}

func apiTokenFromRequest(c *gin.Context) (string, bool) {
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("bearer "):]), false
	}
	token := strings.TrimSpace(c.GetHeader("Token"))
	if token == "" {
		return "", false
	}
	if legacyTokenHeaderExpired(apiTokenNow()) {
		c.Set(legacyTokenHeaderExpiredKey, true)
		return "", true
	}
	return token, true
}

func legacyTokenHeaderExpired(now time.Time) bool {
	return !now.Before(legacyTokenHeaderSunsetAt)
}
