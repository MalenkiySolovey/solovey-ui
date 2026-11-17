// Package bearer owns cached API-token HTTP admission and dispatcher scope policy.
package bearer

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/audit"
	"github.com/MalenkiySolovey/solovey-ui/backend/http/response"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
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

// Commands preserves current durable account authorization behind each cache hit.
type Commands interface {
	HashAPIToken(string) (string, error)
	AuthorizeAPIToken(context.Context, uint, string, int64) (panelsecurity.APITokenAuthorization, error)
	RecordTokenUse(uint, string) error
}
type Deps struct {
	Users        Commands
	LoadTokens   func() ([]byte, error)
	Now          func() time.Time
	ClientIP     func(*gin.Context) string
	Audit        func(*gin.Context, string, string, string, string, map[string]any)
	RequireScope func(*gin.Context, string, ...string) bool
	Warning      func(...any)
	Error        func(...any)
}
type Handler struct {
	Deps
	tokensMu      sync.RWMutex
	tokens        map[string]TokenInMemory
	tokenRevision uint64
}

func New(deps Deps) *Handler { return &Handler{Deps: deps, tokens: map[string]TokenInMemory{}} }
func (a *Handler) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
func (a *Handler) warning(args ...any) {
	if a.Warning != nil {
		a.Warning(args...)
	}
}
func (a *Handler) logError(args ...any) {
	if a.Error != nil {
		a.Error(args...)
	}
}

const (
	UsernameKey            = "apiUsername"
	ScopeKey               = "apiTokenScope"
	LegacyHeaderExpiredKey = "legacyTokenHeaderExpired"
	// #nosec G101 -- a sunset date string, not a credential.
	LegacyHeaderSunset = "Sat, 15 Aug 2026 00:00:00 GMT"
)

var (
	legacyHeaderSunsetAt = time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
)

// actionScopes maps each apiv2 dispatcher action to the API-token scopes
// permitted to invoke it. "admin" is always allowed (added in EnforceActionScope)
// and is also the default token scope, so this only constrains tokens an admin
// deliberately narrowed. Every dispatcher action is listed even when the
// destination handler repeats the same check; an accidentally added action
// must fail closed until its scope policy is explicit. Separately registered
// component, security, and audit routes retain their own scope gates.
var actionScopes = map[string][]string{
	// State mutations and active probes require write.
	"save":            {"write"},
	"reorder":         {"write"},
	"restartApp":      {"write"},
	"restartSb":       {"write"},
	"checkOutbound":   {"write"},
	"getCertPing":     {"write"},
	"resetTraffic":    {"write"},
	"importdb":        {"database"},
	"rotateSubSecret": {"write"},
	"linkConvert":     {"read", "write"},
	"subConvert":      {"read", "write"},
	// Config / identity / secret reads — component scopes are excluded.
	"load":          {"read", "write"},
	"capabilities":  {"read", "write"},
	"inbounds":      {"read", "write"},
	"inboundDrafts": {"read", "write"},
	"outbounds":     {"read", "write"},
	"endpoints":     {"read", "write"},
	"services":      {"read", "write"},
	"tls":           {"read", "write"},
	"clients":       {"read", "write"},
	"config":        {"read", "write"},
	"users":         {"read", "write"},
	"settings":      {"read", "write"},
	"changes":       {"read", "write"},
	"keypairs":      {"read", "write"},
	// Operational metrics — observability tokens may read these.
	"stats":   {"read", "write", "observability"},
	"status":  {"read", "write", "observability"},
	"onlines": {"read", "write", "observability"},
	"logs":    {"read", "write", "observability"},
	"getdb":   {"database"},
}

// EnforceActionScope applies the per-action scope policy for the apiv2 action
// dispatchers. Actions absent from the policy map fail closed for bearer-token
// requests. On denial it writes a 403 and returns false.
func (a *Handler) EnforceActionScope(c *gin.Context, action string) bool {
	allowed, ok := actionScopes[action]
	if !ok {
		c.AbortWithStatusJSON(http.StatusForbidden, response.Msg{Success: false, Msg: "API token scope policy is not declared"})
		return false
	}
	if a.RequireScope == nil {
		c.AbortWithStatus(http.StatusForbidden)
		return false
	}
	return a.RequireScope(c, action, append([]string{"admin"}, allowed...)...)
}

func (a *Handler) FindUsername(c *gin.Context) string {
	token, legacyHeader := TokenFromRequest(c, a.now())
	if token == "" {
		return ""
	}
	if a.Users == nil || a.ClientIP == nil || a.Audit == nil {
		return ""
	}
	tokenHash, err := a.Users.HashAPIToken(token)
	if err != nil {
		a.warning("unable to hash API token:", err)
		return ""
	}
	now := a.now().Unix()
	a.tokensMu.RLock()
	t, ok := a.tokens[tokenHash]
	revision := a.tokenRevision
	a.tokensMu.RUnlock()
	if !ok {
		return ""
	}
	if !t.Enabled {
		return ""
	}
	if t.Expiry > 0 && t.Expiry <= now {
		return ""
	}
	principal, err := a.Users.AuthorizeAPIToken(c.Request.Context(), t.ID, tokenHash, now)
	if err != nil {
		return ""
	}
	// A reload clears authorization immediately. A decision that began before
	// that boundary cannot authorize from the earlier snapshot after it.
	a.tokensMu.RLock()
	current, exists := a.tokens[tokenHash]
	currentRevision := a.tokenRevision
	a.tokensMu.RUnlock()
	if revision != currentRevision || !exists || current.ID != t.ID {
		return ""
	}
	if legacyHeader {
		c.Header("Deprecation", "true")
		c.Header("Sunset", LegacyHeaderSunset)
		a.Audit(c, principal.Username, "legacy_token_header_used", "api_token", audit.SeverityWarn, map[string]any{
			"tokenPrefix": principal.TokenPrefix,
			"sunset":      LegacyHeaderSunset,
		})
	}
	_ = a.Users.RecordTokenUse(t.ID, a.ClientIP(c))
	c.Set(ScopeKey, principal.Scope)
	return principal.Username
}

func (a *Handler) CheckToken(c *gin.Context) {
	username := a.FindUsername(c)
	if username != "" {
		c.Set(UsernameKey, username)
		c.Next()
		return
	}
	if c.GetBool(LegacyHeaderExpiredKey) {
		c.Header("Deprecation", "true")
		c.Header("Sunset", LegacyHeaderSunset)
		c.JSON(http.StatusUnauthorized, response.Msg{
			Success: false,
			Msg:     "legacy token header expired",
		})
		c.Abort()
		return
	}
	c.JSON(http.StatusUnauthorized, response.Msg{
		Success: false,
		Msg:     "invalid token",
	})
	c.Abort()
}

func (a *Handler) ReloadTokens() {
	a.tokensMu.Lock()
	a.tokenRevision++
	revision := a.tokenRevision
	a.tokens = map[string]TokenInMemory{}
	a.tokensMu.Unlock()
	loader := a.LoadTokens
	if loader == nil {
		a.logError("unable to load tokens: token loader is unavailable")
		return
	}
	tokens, err := loader()
	if err != nil {
		a.logError("unable to load tokens: ", err)
		return
	}
	var loaded []TokenInMemory
	if len(tokens) > 0 {
		if err := json.Unmarshal(tokens, &loaded); err != nil {
			a.logError("unable to load tokens: ", err)
			return
		}
	}
	newMap := make(map[string]TokenInMemory, len(loaded))
	for _, t := range loaded {
		newMap[t.TokenHash] = t
	}
	a.tokensMu.Lock()
	if a.tokenRevision == revision {
		a.tokens = newMap
	}
	a.tokensMu.Unlock()
}

func TokenFromRequest(c *gin.Context, now time.Time) (string, bool) {
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("bearer "):]), false
	}
	token := strings.TrimSpace(c.GetHeader("Token"))
	if token == "" {
		return "", false
	}
	if legacyTokenHeaderExpired(now) {
		c.Set(LegacyHeaderExpiredKey, true)
		return "", true
	}
	return token, true
}

func legacyTokenHeaderExpired(now time.Time) bool {
	return !now.Before(legacyHeaderSunsetAt)
}
