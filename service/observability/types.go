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

var (
	IsValidObservabilityMetric = canonical.IsValidObservabilityMetric
	ParseObservabilityMetric   = canonical.ParseObservabilityMetric
	IsValidObservabilityBucket = canonical.IsValidObservabilityBucket
	ParseObservabilityBucket   = canonical.ParseObservabilityBucket
)
