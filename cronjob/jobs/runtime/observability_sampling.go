package runtime

import (
	"sync"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	observabilitysvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
)

const (
	observability30sTicks = 15
	observability1mTicks  = 30
	observability5mTicks  = 150
)

type ObservabilitySamplingJob struct {
	service.ObservabilityService

	mu                   sync.Mutex
	ticks                int
	currentObservability func() observabilitysvc.ObservabilitySample
	currentCore          func() observabilitysvc.CoreSample
	now                  func() time.Time
}

func NewObservabilitySamplingJob() *ObservabilitySamplingJob {
	job := &ObservabilitySamplingJob{}
	job.currentObservability = job.ObservabilityService.CurrentObservabilitySample
	job.currentCore = job.ObservabilityService.CurrentCoreSample
	job.now = time.Now
	return job
}

func (j *ObservabilitySamplingJob) Run() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if err := j.RecordObservabilitySample(observabilitysvc.ObservabilityBucket2s, j.currentObservability()); err != nil {
		logger.Warning("record observability sample failed:", err)
		return
	}
	if err := j.RecordCoreSample(observabilitysvc.ObservabilityBucket2s, j.currentCore()); err != nil {
		logger.Warning("record core observability sample failed:", err)
		return
	}
	j.ticks++

	j.aggregateEvery(observabilitysvc.ObservabilityBucket30s, observability30sTicks)
	j.aggregateEvery(observabilitysvc.ObservabilityBucket1m, observability1mTicks)
	j.aggregateEvery(observabilitysvc.ObservabilityBucket5m, observability5mTicks)
}

func (j *ObservabilitySamplingJob) aggregateEvery(bucket observabilitysvc.ObservabilityBucket, interval int) {
	if interval <= 0 || j.ticks%interval != 0 {
		return
	}
	samples, err := j.HistoryForBucket(observabilitysvc.ObservabilityBucket2s)
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
	if err := j.RecordObservabilitySample(bucket, observabilitysvc.AggregateObservabilitySamples(samples, ts)); err != nil {
		logger.Warning("record aggregated observability sample failed:", err)
	}

	coreSamples, err := j.CoreHistoryForBucket(observabilitysvc.ObservabilityBucket2s)
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
	if err := j.RecordCoreSample(bucket, observabilitysvc.AggregateCoreSamples(coreSamples, ts)); err != nil {
		logger.Warning("record aggregated core sample failed:", err)
	}
}
