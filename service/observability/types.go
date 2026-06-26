package observability

import (
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"time"
)

type ObservabilityBucket string
type ObservabilityMetric string

const (
	ObservabilityBucket2s             ObservabilityBucket = "2s"
	ObservabilityBucket30s            ObservabilityBucket = "30s"
	ObservabilityBucket1m             ObservabilityBucket = "1m"
	ObservabilityBucket5m             ObservabilityBucket = "5m"
	ObservabilityMetricCPU            ObservabilityMetric = "cpu"
	ObservabilityMetricRAM            ObservabilityMetric = "ram"
	ObservabilityMetricNetIn          ObservabilityMetric = "net_in"
	ObservabilityMetricNetOut         ObservabilityMetric = "net_out"
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

func DefaultBucketCap(bucket ObservabilityBucket) int {
	return observabilityDefaultBucketCaps[bucket]
}

func CapsForMemory(capMB int) map[ObservabilityBucket]int {
	return capsForObservabilityMemory(capMB)
}

var observabilityDefaultBucketCaps = map[ObservabilityBucket]int{
	ObservabilityBucket2s:  300,
	ObservabilityBucket30s: 240,
	ObservabilityBucket1m:  240,
	ObservabilityBucket5m:  144,
}

type ObservabilitySample struct {
	DateTime int64                  `json:"dateTime"`
	CPU      float64                `json:"cpu"`
	Memory   map[string]interface{} `json:"memory"`
	Network  map[string]interface{} `json:"network"`
}
type CoreSample struct {
	DateTime int64                  `json:"dateTime"`
	Core     map[string]interface{} `json:"core"`
}
type ObservabilityMetricSample struct {
	DateTime int64   `json:"dateTime"`
	Value    float64 `json:"value"`
}

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
