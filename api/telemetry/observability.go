package telemetry

import (
	"net/http"
	"strconv"

	observabilitysvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func (a *Handler) GetObservabilityHistory(c *gin.Context) {
	if !a.RequireScope(c, "observability", "observability", "admin") {
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
		samples, err := a.ObservabilityService.MetricHistory(metric, bucket, since)
		a.JSONObj(c, gin.H{
			"bucket":  bucket,
			"metric":  metric,
			"samples": samples,
		}, err)
		return
	}
	samples, err := a.ObservabilityService.HistoryForBucketSince(bucket, since)
	a.JSONObj(c, gin.H{
		"bucket":  bucket,
		"samples": samples,
	}, err)
}

func (a *Handler) GetCoreHistory(c *gin.Context) {
	if !a.RequireScope(c, "observability", "observability", "admin") {
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
	samples, err := a.ObservabilityService.CoreHistoryForBucketSince(bucket, since)
	a.JSONObj(c, gin.H{
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
