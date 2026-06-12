package api

import (
	"net/http"
	"strconv"
	"strings"
	stdatomic "sync/atomic"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

const (
	wsTokenTTL    = 60 * time.Second
	wsTokenPrefix = "sui.token."
)

var legacyWSProtocolAuditWarned stdatomic.Bool

func (a *ApiService) IssueWSToken(c *gin.Context) {
	if !a.enforceWSHandshakeRateLimit(c, "ws-token") {
		return
	}
	user := GetLoginUser(c)
	if user == "" {
		jsonMsg(c, "wsToken", common.NewError("invalid login"))
		return
	}
	if !a.validateWSOrigin(c, user) {
		return
	}
	now := time.Now()
	expiresAt := now.Add(wsTokenTTL)
	token := common.Random(32)
	wsTokens.Lock()
	maybeSweepWSTokensLocked(now)
	wsTokens.tokens[wsTokenDigest(token)] = realtimeToken{user: user, expiresAt: expiresAt}
	enforceWSTokenCapLocked()
	scheduleWSTokenSweepLocked()
	wsTokens.Unlock()
	jsonObj(c, gin.H{
		"token":     token,
		"expiresAt": expiresAt.Unix(),
	}, nil)
}

func (a *ApiService) enforceWSHandshakeRateLimit(c *gin.Context, endpoint string) bool {
	err := checkWSHandshakeRateLimit(wsHandshakeRateLimitKey(endpoint, getRemoteIp(c)))
	if err == nil {
		return true
	}
	a.recordAudit(c, "", "ws_rate_limited", "realtime", service.AuditSeverityWarn, map[string]any{
		"endpoint": endpoint,
	})
	c.Header("Retry-After", strconv.Itoa(int(wsHandshakeRateLimitWindow/time.Second)))
	if endpoint == "ws-token" {
		c.JSON(http.StatusTooManyRequests, Msg{Success: false, Msg: "wsToken: " + err.Error()})
	} else {
		c.Status(http.StatusTooManyRequests)
	}
	return false
}

func wsTokenFromRequest(c *gin.Context) (string, bool) {
	if token := strings.TrimSpace(c.Query("token")); token != "" {
		return token, false
	}
	var legacy string
	for _, part := range strings.Split(c.GetHeader("Sec-WebSocket-Protocol"), ",") {
		part = strings.TrimSpace(part)
		if token, ok := strings.CutPrefix(part, wsTokenPrefix); ok && token != "" {
			return token, false
		}
		if part != "" && part != wsSubprotocol && legacy == "" {
			legacy = part
		}
	}
	if legacy != "" {
		return legacy, true
	}
	return "", false
}

func (a *ApiService) recordLegacyWSProtocolAuditOnce(c *gin.Context, user string) {
	if !legacyWSProtocolAuditWarned.CompareAndSwap(false, true) {
		return
	}
	a.recordAudit(c, user, "ws_protocol_deprecated", "realtime", service.AuditSeverityWarn, map[string]any{
		"format": "legacy_token_subprotocol",
	})
}
