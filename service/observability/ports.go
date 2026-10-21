//go:build !minimal

package observability

// Sampler is the existing bounded sampling/history owner contract.
type Sampler interface {
	CurrentObservabilitySample() ObservabilitySample
	CurrentCoreSample() CoreSample
	RecordObservabilitySample(ObservabilityBucket, ObservabilitySample) error
	RecordCoreSample(ObservabilityBucket, CoreSample) error
	HistoryForBucket(ObservabilityBucket) ([]ObservabilitySample, error)
	CoreHistoryForBucket(ObservabilityBucket) ([]CoreSample, error)
}

// History exposes existing read projections without granting ingestion or pruning.
type History interface {
	HistoryForBucketSince(ObservabilityBucket, int64) ([]ObservabilitySample, error)
	CoreHistoryForBucketSince(ObservabilityBucket, int64) ([]CoreSample, error)
	MetricHistory(ObservabilityMetric, ObservabilityBucket, int64) ([]ObservabilityMetricSample, error)
}
