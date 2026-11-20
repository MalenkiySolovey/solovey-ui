//go:build !minimal

package observability

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type PanelService struct {
	service.ServerService
	service.SettingService
}

func (s *PanelService) implementation() *Service {
	return &Service{
		Snapshots: s,
		Store:     SharedStore(),
		Settings:  &s.SettingService,
		Warning:   logger.Warning,
		Warningf:  logger.Warningf,
	}
}

func (s *PanelService) CurrentObservabilitySample() ObservabilitySample {
	return ObservabilitySample{
		DateTime: time.Now().Unix(),
		CPU:      s.ServerService.GetCpuPercent(),
		Memory:   s.ServerService.GetMemInfo(),
		Network:  s.ServerService.GetNetInfo(),
	}
}

func (s *PanelService) CurrentCoreSample() CoreSample {
	return CoreSample{
		DateTime: time.Now().Unix(),
		Core:     s.ServerService.GetSingboxInfo(),
	}
}

func (s *PanelService) RecordObservabilitySample(bucket ObservabilityBucket, sample ObservabilitySample) error {
	return s.implementation().RecordObservabilitySample(bucket, sample)
}

func (s *PanelService) RecordCoreSample(bucket ObservabilityBucket, sample CoreSample) error {
	return s.implementation().RecordCoreSample(bucket, sample)
}

func (s *PanelService) HistoryForBucket(bucket ObservabilityBucket) ([]ObservabilitySample, error) {
	return s.implementation().HistoryForBucket(bucket)
}

func (s *PanelService) HistoryForBucketSince(bucket ObservabilityBucket, since int64) ([]ObservabilitySample, error) {
	return s.implementation().HistoryForBucketSince(bucket, since)
}

func (s *PanelService) CoreHistoryForBucket(bucket ObservabilityBucket) ([]CoreSample, error) {
	return s.implementation().CoreHistoryForBucket(bucket)
}

func (s *PanelService) CoreHistoryForBucketSince(bucket ObservabilityBucket, since int64) ([]CoreSample, error) {
	return s.implementation().CoreHistoryForBucketSince(bucket, since)
}

func (s *PanelService) MetricHistory(metric ObservabilityMetric, bucket ObservabilityBucket, since int64) ([]ObservabilityMetricSample, error) {
	return s.implementation().MetricHistory(metric, bucket, since)
}
