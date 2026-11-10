//go:build !minimal

package service

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/logger"
	observabilityimpl "github.com/MalenkiySolovey/solovey-ui/service/observability"
)

type ObservabilityService struct {
	ServerService
	SettingService
}

func (s *ObservabilityService) implementation() *observabilityimpl.Service {
	return &observabilityimpl.Service{
		Snapshots: s,
		Store:     observabilityimpl.SharedStore(),
		Settings:  &s.SettingService,
		Warning:   logger.Warning,
		Warningf:  logger.Warningf,
	}
}

func (s *ObservabilityService) CurrentObservabilitySample() observabilityimpl.ObservabilitySample {
	return observabilityimpl.ObservabilitySample{
		DateTime: time.Now().Unix(),
		CPU:      s.ServerService.GetCpuPercent(),
		Memory:   s.ServerService.GetMemInfo(),
		Network:  s.ServerService.GetNetInfo(),
	}
}

func (s *ObservabilityService) CurrentCoreSample() observabilityimpl.CoreSample {
	return observabilityimpl.CoreSample{
		DateTime: time.Now().Unix(),
		Core:     s.ServerService.GetSingboxInfo(),
	}
}

func (s *ObservabilityService) History() []observabilityimpl.ObservabilitySample {
	return s.implementation().History()
}

func (s *ObservabilityService) CoreHistory() []observabilityimpl.CoreSample {
	return s.implementation().CoreHistory()
}

func (s *ObservabilityService) RecordObservabilitySample(bucket observabilityimpl.ObservabilityBucket, sample observabilityimpl.ObservabilitySample) error {
	return s.implementation().RecordObservabilitySample(bucket, sample)
}

func (s *ObservabilityService) RecordCoreSample(bucket observabilityimpl.ObservabilityBucket, sample observabilityimpl.CoreSample) error {
	return s.implementation().RecordCoreSample(bucket, sample)
}

func (s *ObservabilityService) HistoryForBucket(bucket observabilityimpl.ObservabilityBucket) ([]observabilityimpl.ObservabilitySample, error) {
	return s.implementation().HistoryForBucket(bucket)
}

func (s *ObservabilityService) HistoryForBucketSince(bucket observabilityimpl.ObservabilityBucket, since int64) ([]observabilityimpl.ObservabilitySample, error) {
	return s.implementation().HistoryForBucketSince(bucket, since)
}

func (s *ObservabilityService) CoreHistoryForBucket(bucket observabilityimpl.ObservabilityBucket) ([]observabilityimpl.CoreSample, error) {
	return s.implementation().CoreHistoryForBucket(bucket)
}

func (s *ObservabilityService) CoreHistoryForBucketSince(bucket observabilityimpl.ObservabilityBucket, since int64) ([]observabilityimpl.CoreSample, error) {
	return s.implementation().CoreHistoryForBucketSince(bucket, since)
}

func (s *ObservabilityService) MetricHistory(metric observabilityimpl.ObservabilityMetric, bucket observabilityimpl.ObservabilityBucket, since int64) ([]observabilityimpl.ObservabilityMetricSample, error) {
	return s.implementation().MetricHistory(metric, bucket, since)
}
