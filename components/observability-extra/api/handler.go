//go:build !minimal

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/auditquery"
	"github.com/MalenkiySolovey/solovey-ui/service"
	observabilitysvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/gin-gonic/gin"
)

type Deps struct {
	ObservabilityService   service.ObservabilityService
	AuditService           service.AuditService
	RequireScope           func(*gin.Context, string, ...string) bool
	RequireAuditAdminScope func(*gin.Context) bool
	JSONObj                func(*gin.Context, interface{}, error)
	Actor                  func(*gin.Context) string
	RemoteIP               func(*gin.Context) string
	CheckAuditRateLimit    func(string) error
	AuditRateLimitKey      func(string, string) string
	AuditRateLimitWindow   time.Duration
	Audit                  func(*gin.Context, string, string, string, string, map[string]any)
}

type Handler struct {
	ObservabilityService   service.ObservabilityService
	AuditService           service.AuditService
	RequireScope           func(*gin.Context, string, ...string) bool
	RequireAuditAdminScope func(*gin.Context) bool
	JSONObj                func(*gin.Context, interface{}, error)
	Actor                  func(*gin.Context) string
	RemoteIP               func(*gin.Context) string
	CheckAuditRateLimit    func(string) error
	AuditRateLimitKey      func(string, string) string
	AuditRateLimitWindow   time.Duration
	Audit                  func(*gin.Context, string, string, string, string, map[string]any)
}

func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	handler := NewHandler(deps)
	security := g.Group("/security")
	security.GET("/audit", handler.GetSecurityAudit)

	observability := g.Group("/observability")
	observability.GET("/history", handler.GetObservabilityHistory)
	observability.GET("/core-history", handler.GetCoreHistory)
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		ObservabilityService:   deps.ObservabilityService,
		AuditService:           deps.AuditService,
		RequireScope:           deps.RequireScope,
		RequireAuditAdminScope: deps.RequireAuditAdminScope,
		JSONObj:                deps.JSONObj,
		Actor:                  deps.Actor,
		RemoteIP:               deps.RemoteIP,
		CheckAuditRateLimit:    deps.CheckAuditRateLimit,
		AuditRateLimitKey:      deps.AuditRateLimitKey,
		AuditRateLimitWindow:   deps.AuditRateLimitWindow,
		Audit:                  deps.Audit,
	}
}

type Envelope struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

func (h *Handler) GetSecurityAudit(c *gin.Context) {
	if h.RequireAuditAdminScope == nil || !h.RequireAuditAdminScope(c) {
		return
	}
	if !h.enforceAuditEndpointRateLimit(c) {
		return
	}
	limit, err := auditquery.Limit(c.Query("limit"), 200, 200)
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	cursor, err := auditquery.Cursor(c.Query("cursor"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	eventFilter, err := auditquery.Event(c.Query("event"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	severityFilter, err := auditquery.Severity(c.Query("severity"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	since, err := auditquery.UnixSeconds("since", c.Query("since"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	until, err := auditquery.UnixSeconds("until", c.Query("until"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return
	}
	events, nextCursor, err := h.AuditService.ListPageFiltered(cursor, limit, eventFilter, severityFilter, since, until)
	h.JSONObj(c, gin.H{"events": events, "nextCursor": nextCursor, "limit": limit}, err)
}

func (h *Handler) enforceAuditEndpointRateLimit(c *gin.Context) bool {
	if h.Actor == nil || h.RemoteIP == nil || h.CheckAuditRateLimit == nil || h.AuditRateLimitKey == nil {
		c.JSON(http.StatusServiceUnavailable, Envelope{Success: false, Msg: "audit: security boundary unavailable"})
		return false
	}
	actor, ip := h.Actor(c), h.RemoteIP(c)
	if actor == "" {
		actor = "unknown"
	}
	if ip == "" {
		ip = "unknown"
	}
	if err := h.CheckAuditRateLimit(h.AuditRateLimitKey(actor, ip)); err == nil {
		return true
	} else {
		if h.Audit != nil {
			h.Audit(c, actor, "audit_rate_limited", "audit", service.AuditSeverityWarn, map[string]any{"ip": ip})
		}
		c.Header("Retry-After", strconv.Itoa(int(h.AuditRateLimitWindow/time.Second)))
		c.JSON(http.StatusTooManyRequests, Envelope{Success: false, Msg: "audit: " + err.Error()})
		return false
	}
}

func (h *Handler) GetObservabilityHistory(c *gin.Context) {
	if !h.RequireScope(c, "observability", "observability", "admin") {
		return
	}
	bucket, since, ok := parseObservabilityQuery(c)
	if !ok {
		return
	}
	if metricRaw := c.Query("metric"); metricRaw != "" {
		metric, err := observabilitysvc.ParseObservabilityMetric(metricRaw)
		if err != nil {
			c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "observability: " + err.Error()})
			return
		}
		samples, err := h.ObservabilityService.MetricHistory(metric, bucket, since)
		h.JSONObj(c, gin.H{
			"bucket":  bucket,
			"metric":  metric,
			"samples": samples,
		}, err)
		return
	}
	samples, err := h.ObservabilityService.HistoryForBucketSince(bucket, since)
	h.JSONObj(c, gin.H{
		"bucket":  bucket,
		"samples": samples,
	}, err)
}

func (h *Handler) GetCoreHistory(c *gin.Context) {
	if !h.RequireScope(c, "observability", "observability", "admin") {
		return
	}
	if c.Query("metric") != "" {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "observability: metric is not supported for core history"})
		return
	}
	bucket, since, ok := parseObservabilityQuery(c)
	if !ok {
		return
	}
	samples, err := h.ObservabilityService.CoreHistoryForBucketSince(bucket, since)
	h.JSONObj(c, gin.H{
		"bucket":  bucket,
		"samples": samples,
	}, err)
}

func parseObservabilityQuery(c *gin.Context) (observabilitysvc.ObservabilityBucket, int64, bool) {
	bucket, err := observabilitysvc.ParseObservabilityBucket(c.Query("bucket"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "observability: " + err.Error()})
		return "", 0, false
	}
	since, err := parseObservabilitySince(c.Query("since"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "observability: " + err.Error()})
		return "", 0, false
	}
	return bucket, since, true
}

func parseObservabilitySince(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	since, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || since < 0 {
		return 0, common.NewError("invalid since")
	}
	return since, nil
}
