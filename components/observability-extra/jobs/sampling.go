//go:build !minimal

package jobs

import (
	"context"
	"sync"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	observabilitysvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
)

const (
	observability30sTicks = 15
	observability1mTicks  = 30
	observability5mTicks  = 150
)

type SamplingJob struct {
	mu                   sync.Mutex
	sampler              Sampler
	ticks                int
	currentObservability func() observabilitysvc.ObservabilitySample
	currentCore          func() observabilitysvc.CoreSample
	now                  func() time.Time
}

type Sampler interface {
	CurrentObservabilitySample() observabilitysvc.ObservabilitySample
	CurrentCoreSample() observabilitysvc.CoreSample
	RecordObservabilitySample(observabilitysvc.ObservabilityBucket, observabilitysvc.ObservabilitySample) error
	RecordCoreSample(observabilitysvc.ObservabilityBucket, observabilitysvc.CoreSample) error
	HistoryForBucket(observabilitysvc.ObservabilityBucket) ([]observabilitysvc.ObservabilitySample, error)
	CoreHistoryForBucket(observabilitysvc.ObservabilityBucket) ([]observabilitysvc.CoreSample, error)
}

func NewSamplingJob(sampler Sampler) *SamplingJob {
	job := &SamplingJob{sampler: sampler}
	job.currentObservability = sampler.CurrentObservabilitySample
	job.currentCore = sampler.CurrentCoreSample
	job.now = time.Now
	return job
}

func (j *SamplingJob) Run() {
	j.RunContext(context.Background())
}

func (j *SamplingJob) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if ctx.Err() != nil {
		return
	}

	if err := j.sampler.RecordObservabilitySample(observabilitysvc.ObservabilityBucket2s, j.currentObservability()); err != nil {
		logger.Warning("record observability sample failed:", err)
		return
	}
	if ctx.Err() != nil {
		return
	}
	if err := j.sampler.RecordCoreSample(observabilitysvc.ObservabilityBucket2s, j.currentCore()); err != nil {
		logger.Warning("record core observability sample failed:", err)
		return
	}
	j.ticks++

	j.aggregateEvery(ctx, observabilitysvc.ObservabilityBucket30s, observability30sTicks)
	j.aggregateEvery(ctx, observabilitysvc.ObservabilityBucket1m, observability1mTicks)
	j.aggregateEvery(ctx, observabilitysvc.ObservabilityBucket5m, observability5mTicks)
}

func (j *SamplingJob) aggregateEvery(ctx context.Context, bucket observabilitysvc.ObservabilityBucket, interval int) {
	if ctx.Err() != nil || interval <= 0 || j.ticks%interval != 0 {
		return
	}
	samples, err := j.sampler.HistoryForBucket(observabilitysvc.ObservabilityBucket2s)
	if err != nil {
		logger.Warning("read observability samples for aggregation failed:", err)
		return
	}
	if len(samples) == 0 {
		return
	}
	if len(samples) > interval {
		samples = samples[len(samples)-interval:]
	}
	ts := j.now().Unix()
	if err := j.sampler.RecordObservabilitySample(bucket, observabilitysvc.AggregateObservabilitySamples(samples, ts)); err != nil {
		logger.Warning("record aggregated observability sample failed:", err)
	}
	if ctx.Err() != nil {
		return
	}

	coreSamples, err := j.sampler.CoreHistoryForBucket(observabilitysvc.ObservabilityBucket2s)
	if err != nil {
		logger.Warning("read core samples for aggregation failed:", err)
		return
	}
	if len(coreSamples) == 0 {
		return
	}
	if len(coreSamples) > interval {
		coreSamples = coreSamples[len(coreSamples)-interval:]
	}
	if err := j.sampler.RecordCoreSample(bucket, observabilitysvc.AggregateCoreSamples(coreSamples, ts)); err != nil {
		logger.Warning("record aggregated core sample failed:", err)
	}
}
