package importxui

import (
	"context"
	"net"
	"net/http"
	"time"

	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"

	"github.com/gin-gonic/gin"
)

const (
	xuiRequestWindow  = time.Minute
	RequestLimit      = 5
	xuiRequestTimeout = 10 * time.Minute
	xuiRateMaxEntries = 4096
)

var xuiRequestRateLimiter = ratelimit.NewFixedWindow[string](xuiRequestWindow, RequestLimit, xuiRateMaxEntries, xuiRequestWindow)

func init() {
	dbhooks.RegisterResetHook("api.xui_rates", ResetRateLimits)
}

// ResetRateLimits clears per-caller import throttling when runtime state resets.
func ResetRateLimits() {
	xuiRequestRateLimiter.ResetAll()
}

func (a *Handler) beginRequest(c *gin.Context) (context.Context, context.CancelFunc, bool) {
	if !a.RequireScope(c, "database", "admin") {
		return c.Request.Context(), func() {}, false
	}
	if !a.enforceRateLimit(c) {
		return c.Request.Context(), func() {}, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), xuiRequestTimeout)
	c.Request = c.Request.WithContext(ctx)
	return ctx, cancel, true
}

// connContextKey carries each accepted net.Conn through the request context so
// extendSlowRequestDeadlines can lift the connection's deadlines directly.
type connContextKey struct{}

// SaveConnContext stashes the accepted connection into its context. Wire it as
// http.Server.ConnContext so the long-running import handlers can reach the raw
// net.Conn.
//
// This indirection is required: the global gzip middleware replaces c.Writer
// with a wrapper that embeds the gin.ResponseWriter *interface* (which has no
// Unwrap method), so http.NewResponseController(c.Writer) cannot traverse to
// the connection and SetWriteDeadline silently returns ErrNotSupported. Without
// the raw conn, the 30s WriteTimeout would still sever a slow import mid-write,
// which the browser surfaces as "Network Error".
func SaveConnContext(ctx context.Context, conn net.Conn) context.Context {
	return context.WithValue(ctx, connContextKey{}, conn)
}

func connFromContext(ctx context.Context) (net.Conn, bool) {
	conn, ok := ctx.Value(connContextKey{}).(net.Conn)
	return conn, ok
}

// extendSlowRequestDeadlines lifts the http.Server's 30s Read/Write timeouts
// for a long-running request. Importing a large 3x-ui database can take well
// over 30s; without this the server severs the connection mid-import, so the
// client never receives the result and may resubmit - duplicating the import
// and its pre-import backup (the runaway-backup symptom).
//
// It sets the deadline on the raw net.Conn from SaveConnContext. The
// http.NewResponseController path is only a fallback for setups without the
// ConnContext hook (e.g. tests): under the production gzip middleware it
// no-ops, which is the exact bug this conn-based path fixes.
//
// SECURITY: only ever call this AFTER beginXUIRequest has authenticated,
// scope-checked and rate-limited the caller. Moving it before that auth gate
// would let an unauthenticated client hold a connection open for the whole
// extended window. The deadline is deliberately FINITE (not the zero/no-limit
// value), so even an authorized admin cannot hold the connection indefinitely,
// and the work itself stays bounded by the request context set in
// beginXUIRequest. Unauthenticated callers never reach here: the /api group's
// checkLogin middleware aborts them before any handler runs.
func extendSlowRequestDeadlines(c *gin.Context) {
	deadline := time.Now().Add(xuiRequestTimeout + time.Minute)
	if conn, ok := connFromContext(c.Request.Context()); ok {
		_ = conn.SetReadDeadline(deadline)
		_ = conn.SetWriteDeadline(deadline)
		return
	}
	rc := http.NewResponseController(c.Writer)
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline)
}

func (a *Handler) enforceRateLimit(c *gin.Context) bool {
	key := a.Actor(c)
	if key == "" {
		key = a.RemoteIP(c)
	}
	if !xuiRequestRateLimiter.Allow(key).Allowed {
		a.Audit(c, a.Actor(c), "xui_import_failed", "database", service.AuditSeverityWarn, map[string]any{"reason": "rate_limited"})
		c.JSON(http.StatusTooManyRequests, Envelope{Success: false, Msg: "too many xui import requests"})
		return false
	}
	return true
}
