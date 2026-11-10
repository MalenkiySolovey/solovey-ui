//go:build !minimal

package observability

import canonical "github.com/MalenkiySolovey/solovey-component-observability-extra/history"

type ObservabilityBucket = canonical.ObservabilityBucket
type ObservabilityMetric = canonical.ObservabilityMetric
type ObservabilitySample = canonical.ObservabilitySample
type CoreSample = canonical.CoreSample
type ObservabilityMetricSample = canonical.ObservabilityMetricSample

const (
	ObservabilityBucket2s     = canonical.ObservabilityBucket2s
	ObservabilityBucket30s    = canonical.ObservabilityBucket30s
	ObservabilityBucket1m     = canonical.ObservabilityBucket1m
	ObservabilityBucket5m     = canonical.ObservabilityBucket5m
	ObservabilityMetricCPU    = canonical.ObservabilityMetricCPU
	ObservabilityMetricRAM    = canonical.ObservabilityMetricRAM
	ObservabilityMetricNetIn  = canonical.ObservabilityMetricNetIn
	ObservabilityMetricNetOut = canonical.ObservabilityMetricNetOut
	DefaultMemoryCapMB        = canonical.DefaultMemoryCapMB
	MemoryCapCacheTTL         = canonical.MemoryCapCacheTTL
)

func IsValidObservabilityMetric(metric ObservabilityMetric) bool {
	return canonical.IsValidObservabilityMetric(metric)
}

func ParseObservabilityMetric(raw string) (ObservabilityMetric, error) {
	return canonical.ParseObservabilityMetric(raw)
}

func IsValidObservabilityBucket(bucket ObservabilityBucket) bool {
	return canonical.IsValidObservabilityBucket(bucket)
}

func ParseObservabilityBucket(raw string) (ObservabilityBucket, error) {
	return canonical.ParseObservabilityBucket(raw)
}
