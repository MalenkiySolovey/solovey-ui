package api

import (
	"net/http"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) GetObservabilityHistory(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "observability", "admin", "observability") {
		return
	}
	bucket, since, ok := parseObservabilityQuery(c)
	if !ok {
		return
	}
	if metricRaw := c.Query("metric"); metricRaw != "" {
		metric, err := service.ParseObservabilityMetric(metricRaw)
		if err != nil {
			c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "observability: " + err.Error()})
			return
		}
		samples, err := a.ObservabilityService.MetricHistory(metric, bucket, since)
		jsonObj(c, gin.H{
			"bucket":  bucket,
			"metric":  metric,
			"samples": samples,
		}, err)
		return
	}
	samples, err := a.ObservabilityService.HistoryForBucketSince(bucket, since)
	jsonObj(c, gin.H{
		"bucket":  bucket,
		"samples": samples,
	}, err)
}

func (a *ApiService) GetCoreHistory(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "observability", "admin", "observability") {
		return
	}
	if c.Query("metric") != "" {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "observability: metric is not supported for core history"})
		return
	}
	bucket, since, ok := parseObservabilityQuery(c)
	if !ok {
		return
	}
	samples, err := a.ObservabilityService.CoreHistoryForBucketSince(bucket, since)
	jsonObj(c, gin.H{
		"bucket":  bucket,
		"samples": samples,
	}, err)
}

func parseObservabilityQuery(c *gin.Context) (service.ObservabilityBucket, int64, bool) {
	bucket, err := service.ParseObservabilityBucket(c.Query("bucket"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "observability: " + err.Error()})
		return "", 0, false
	}
	since, err := parseObservabilitySince(c.Query("since"))
	if err != nil {
		c.JSON(http.StatusBadRequest, Msg{Success: false, Msg: "observability: " + err.Error()})
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
