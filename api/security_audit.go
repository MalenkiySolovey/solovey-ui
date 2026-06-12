package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) GetSecurityAudit(c *gin.Context) {
	if !a.requireAuditAdminScope(c) {
		return
	}
	if !a.enforceAuditEndpointRateLimit(c) {
		return
	}
	limit, err := parseAuditLimit(c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	cursor, err := parseAuditCursor(c.Query("cursor"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	eventFilter, err := parseAuditEventFilter(c.Query("event"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	severityFilter, err := parseAuditSeverityFilter(c.Query("severity"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	since, err := parseAuditUnixSecondsFilter("since", c.Query("since"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	until, err := parseAuditUnixSecondsFilter("until", c.Query("until"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	events, nextCursor, err := a.AuditService.ListPageFiltered(cursor, limit, eventFilter, severityFilter, since, until)
	jsonObj(c, gin.H{
		"events":     events,
		"nextCursor": nextCursor,
		"limit":      limit,
	}, err)
}

func parseAuditLimit(raw string) (int, error) {
	if raw == "" {
		return 200, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, common.NewError("invalid limit")
	}
	if limit <= 0 {
		return 0, common.NewError("invalid limit")
	}
	if limit > 200 {
		return 200, nil
	}
	return limit, nil
}

func parseAuditCursor(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, common.NewError("invalid cursor")
	}
	return cursor, nil
}

func parseAuditEventFilter(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > 64 {
		return "", common.NewError("invalid event filter")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return "", common.NewError("invalid event filter")
	}
	return value, nil
}

func parseAuditSeverityFilter(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	switch value {
	case "":
		return "", nil
	case service.AuditSeverityInfo, service.AuditSeverityWarn:
		return value, nil
	default:
		return "", common.NewError("invalid severity filter")
	}
}

func parseAuditUnixSecondsFilter(name string, raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	if len(raw) > 10 {
		return 0, common.NewError("invalid " + name)
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, common.NewError("invalid " + name)
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, common.NewError("invalid " + name)
	}
	return value, nil
}

func (a *ApiService) enforceAuditEndpointRateLimit(c *gin.Context) bool {
	actor := requestActor(c)
	ip := getRemoteIp(c)
	if actor == "" {
		actor = "unknown"
	}
	if ip == "" {
		ip = "unknown"
	}
	err := checkAuditEndpointRateLimit(auditEndpointRateLimitKey(actor, ip))
	if err == nil {
		return true
	}
	a.recordAudit(c, actor, "audit_rate_limited", "audit", service.AuditSeverityWarn, map[string]any{
		"ip": ip,
	})
	c.Header("Retry-After", strconv.Itoa(int(auditEndpointRateLimitWindow/time.Second)))
	c.JSON(http.StatusTooManyRequests, Msg{Success: false, Msg: "audit: " + err.Error()})
	return false
}
