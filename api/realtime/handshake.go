package realtimehttp

import (
	"net/http"
	"strconv"
	"strings"
	stdatomic "sync/atomic"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"

	"github.com/gin-gonic/gin"
)

const (
	wsTokenTTL           = 60 * time.Second
	wsTokenPrefix        = "sui.token."
	handshakeWindow      = time.Minute
	HandshakeLimit       = 30
	handshakeRateMaxKeys = 4096
)

var legacyWSProtocolAuditWarned stdatomic.Bool
var handshakeRateLimiter = ratelimit.NewFixedWindow[string](handshakeWindow, HandshakeLimit, handshakeRateMaxKeys, handshakeWindow)

func (a *Handler) IssueWSToken(c *gin.Context) {
	if !a.EnforceHandshakeRateLimit(c, "ws-token") {
		return
	}
	user := a.LoginUser(c)
	if user == "" {
		a.JSONMsg(c, "wsToken", common.NewError("invalid login"))
		return
	}
	if !a.ValidateOrigin(c, user) {
		return
	}
	now := time.Now()
	expiresAt := now.Add(wsTokenTTL)
	token := common.Random(32)
	StoreToken(token, user, expiresAt)
	a.JSONObj(c, gin.H{
		"token":     token,
		"expiresAt": expiresAt.Unix(),
	}, nil)
}

func (a *Handler) EnforceHandshakeRateLimit(c *gin.Context, endpoint string) bool {
	err := CheckHandshakeRateLimit(HandshakeRateLimitKey(endpoint, a.RemoteIP(c)))
	if err == nil {
		return true
	}
	a.Audit(c, "", "ws_rate_limited", "realtime", service.AuditSeverityWarn, map[string]any{
		"endpoint": endpoint,
	})
	c.Header("Retry-After", strconv.Itoa(int(handshakeWindow/time.Second)))
	if endpoint == "ws-token" {
		c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "msg": "wsToken: " + err.Error()})
	} else {
		c.Status(http.StatusTooManyRequests)
	}
	return false
}

func TokenFromRequest(c *gin.Context) (string, bool) {
	if token := strings.TrimSpace(c.Query("token")); token != "" {
		return token, false
	}
	var legacy string
	for _, part := range strings.Split(c.GetHeader("Sec-WebSocket-Protocol"), ",") {
		part = strings.TrimSpace(part)
		if token, ok := strings.CutPrefix(part, wsTokenPrefix); ok && token != "" {
			return token, false
		}
		if part != "" && part != Subprotocol && legacy == "" {
			legacy = part
		}
	}
	if legacy != "" {
		return legacy, true
	}
	return "", false
}

func (a *Handler) recordLegacyProtocolOnce(c *gin.Context, user string) {
	if !legacyWSProtocolAuditWarned.CompareAndSwap(false, true) {
		return
	}
	a.Audit(c, user, "ws_protocol_deprecated", "realtime", service.AuditSeverityWarn, map[string]any{
		"format": "legacy_token_subprotocol",
	})
}

func CheckHandshakeRateLimit(key string) error {
	if !handshakeRateLimiter.Allow(key).Allowed {
		return common.NewError("too many websocket handshake attempts")
	}
	return nil
}

func HandshakeRateLimitKey(endpoint string, ip string) string { return endpoint + "|" + ip }

func ResetRateLimits() {
	handshakeRateLimiter.ResetAll()
}
