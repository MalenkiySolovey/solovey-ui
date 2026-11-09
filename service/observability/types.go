//go:build !minimal

package observability

import (
	canonical "github.com/MalenkiySolovey/solovey-host/observation"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"time"
)

type ObservabilityBucket = canonical.ObservabilityBucket
type ObservabilityMetric = canonical.ObservabilityMetric

const (
	ObservabilityBucket2s             ObservabilityBucket = canonical.ObservabilityBucket2s
	ObservabilityBucket30s            ObservabilityBucket = canonical.ObservabilityBucket30s
	ObservabilityBucket1m             ObservabilityBucket = canonical.ObservabilityBucket1m
	ObservabilityBucket5m             ObservabilityBucket = canonical.ObservabilityBucket5m
	ObservabilityMetricCPU            ObservabilityMetric = canonical.ObservabilityMetricCPU
	ObservabilityMetricRAM            ObservabilityMetric = canonical.ObservabilityMetricRAM
	ObservabilityMetricNetIn          ObservabilityMetric = canonical.ObservabilityMetricNetIn
	ObservabilityMetricNetOut         ObservabilityMetric = canonical.ObservabilityMetricNetOut
	observabilityDefaultMemoryCapMB                       = 32
	observabilitySampleEstimateBytes                      = 2048
	observabilityCoreSampleBytes                          = 1024
	observabilityWarnMemoryMinSeconds                     = 60
	observabilityMemoryCapCacheTTL                        = 60 * time.Second
)

const (
	DefaultMemoryCapMB = observabilityDefaultMemoryCapMB
	MemoryCapCacheTTL  = observabilityMemoryCapCacheTTL
)

var observabilityDefaultBucketCaps = map[ObservabilityBucket]int{
	ObservabilityBucket2s:  300,
	ObservabilityBucket30s: 240,
	ObservabilityBucket1m:  240,
	ObservabilityBucket5m:  144,
}

type ObservabilitySample = canonical.ObservabilitySample
type CoreSample = canonical.CoreSample
type ObservabilityMetricSample = canonical.ObservabilityMetricSample

func IsValidObservabilityMetric(metric ObservabilityMetric) bool {
	switch metric {
	case ObservabilityMetricCPU, ObservabilityMetricRAM, ObservabilityMetricNetIn, ObservabilityMetricNetOut:
		return true
	default:
		return false
	}
}
func ParseObservabilityMetric(raw string) (ObservabilityMetric, error) {
	metric := ObservabilityMetric(raw)
	if !IsValidObservabilityMetric(metric) {
		return "", common.NewError("invalid observability metric")
	}
	return metric, nil
}
func IsValidObservabilityBucket(bucket ObservabilityBucket) bool {
	_, ok := observabilityDefaultBucketCaps[bucket]
	return ok
}
func ParseObservabilityBucket(raw string) (ObservabilityBucket, error) {
	if raw == "" {
		return ObservabilityBucket2s, nil
	}
	bucket := ObservabilityBucket(raw)
	if !IsValidObservabilityBucket(bucket) {
		return "", common.NewError("invalid observability bucket")
	}
	return bucket, nil
}
