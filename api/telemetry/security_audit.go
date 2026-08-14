package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/auditquery"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) GetSecurityAudit(c *gin.Context) {
	if !a.RequireAuditAdminScope(c) {
		return
	}
	if !a.enforceAuditEndpointRateLimit(c) {
		return
	}
	if !strictAuditQueryKeys(c, "limit", "cursor", "event", "severity", "since", "until") {
		return
	}
	limit, err := parseAuditLimit(c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	cursor, err := parseAuditCursor(c.Query("cursor"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	eventFilter, err := parseAuditEventFilter(c.Query("event"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	severityFilter, err := parseAuditSeverityFilter(c.Query("severity"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	since, err := ParseAuditUnixSecondsFilter("since", c.Query("since"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	until, err := ParseAuditUnixSecondsFilter("until", c.Query("until"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	events, nextCursor, err := a.AuditService.ListPageFiltered(cursor, limit, eventFilter, severityFilter, since, until)
	a.JSONObj(c, gin.H{
		"events":     events,
		"nextCursor": nextCursor,
		"limit":      limit,
	}, err)
}

func (a *Handler) GetRecentSecurityAudit(c *gin.Context) {
	if !a.RequireScope(c, "auditRecent", "admin", "read", "write") {
		return
	}
	if !strictAuditQueryKeys(c, "limit") {
		return
	}
	limit, err := parseAuditLimit(c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	if limit > 25 {
		limit = 25
	}
	events, nextCursor, err := a.AuditService.ListPageFiltered(0, limit, "", "", 0, 0)
	a.JSONObj(c, gin.H{
		"events":     events,
		"nextCursor": nextCursor,
		"limit":      limit,
	}, err)
}

func strictAuditQueryKeys(c *gin.Context, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range c.Request.URL.Query() {
		if _, ok := known[key]; !ok {
			c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: invalid query"})
			return false
		}
	}
	return true
}

func parseAuditLimit(raw string) (int, error) {
	return auditquery.Limit(raw, 200, 200)
}

func parseAuditCursor(raw string) (uint64, error) {
	return auditquery.Cursor(raw)
}

func parseAuditEventFilter(raw string) (string, error) {
	return auditquery.Event(raw)
}

func parseAuditSeverityFilter(raw string) (string, error) {
	return auditquery.Severity(raw)
}

func ParseAuditUnixSecondsFilter(name string, raw string) (int64, error) {
	return auditquery.UnixSeconds(name, raw)
}

func (a *Handler) enforceAuditEndpointRateLimit(c *gin.Context) bool {
	actor := a.Actor(c)
	ip := a.RemoteIP(c)
	if actor == "" {
		actor = "unknown"
	}
	if ip == "" {
		ip = "unknown"
	}
	err := a.CheckAuditRateLimit(a.AuditRateLimitKey(actor, ip))
	if err == nil {
		return true
	}
	a.Audit(c, actor, "audit_rate_limited", "audit", service.AuditSeverityWarn, map[string]any{
		"ip": ip,
	})
	c.Header("Retry-After", strconv.Itoa(int(a.AuditRateLimitWindow/time.Second)))
	c.JSON(http.StatusTooManyRequests, Envelope{Success: false, Msg: "audit: " + err.Error()})
	return false
}
