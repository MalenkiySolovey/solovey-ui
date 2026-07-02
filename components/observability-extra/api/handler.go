//go:build !minimal

package api

import (
	"net/http"
	"strconv"

	telemetryhttp "github.com/MalenkiySolovey/solovey-ui/api/telemetry"
	"github.com/MalenkiySolovey/solovey-ui/service"
	observabilitysvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/gin-gonic/gin"
)

type Deps struct {
	Telemetry            telemetryhttp.Deps
	ObservabilityService service.ObservabilityService
}

type Handler struct {
	ObservabilityService service.ObservabilityService
	RequireScope         func(*gin.Context, string, ...string) bool
	JSONObj              func(*gin.Context, interface{}, error)
}

func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	telemetryHandler := telemetryhttp.NewHandler(deps.Telemetry)
	security := g.Group("/security")
	security.GET("/audit", telemetryHandler.GetSecurityAudit)

	handler := NewHandler(deps)
	observability := g.Group("/observability")
	observability.GET("/history", handler.GetObservabilityHistory)
	observability.GET("/core-history", handler.GetCoreHistory)
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		ObservabilityService: deps.ObservabilityService,
		RequireScope:         deps.Telemetry.RequireScope,
		JSONObj:              deps.Telemetry.JSONObj,
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
			c.JSON(http.StatusBadRequest, telemetryhttp.Envelope{Success: false, Msg: "observability: " + err.Error()})
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
		c.JSON(http.StatusBadRequest, telemetryhttp.Envelope{Success: false, Msg: "observability: metric is not supported for core history"})
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
		c.JSON(http.StatusBadRequest, telemetryhttp.Envelope{Success: false, Msg: "observability: " + err.Error()})
		return "", 0, false
	}
	since, err := parseObservabilitySince(c.Query("since"))
	if err != nil {
		c.JSON(http.StatusBadRequest, telemetryhttp.Envelope{Success: false, Msg: "observability: " + err.Error()})
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
