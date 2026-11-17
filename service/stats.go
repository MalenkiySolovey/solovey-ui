package service

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/backend/accounting"
	"github.com/MalenkiySolovey/solovey-ui/backend/realtime"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	coretracker "github.com/MalenkiySolovey/solovey-ui/core/tracker"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

type onlines struct {
	Inbound        []string                              `json:"inbound,omitempty"`
	User           []string                              `json:"user,omitempty"`
	Outbound       []string                              `json:"outbound,omitempty"`
	OutboundHealth map[string]coreruntime.OutboundHealth `json:"outboundHealth"`
}

type StatsService struct {
	Runtime *Runtime
}

func (s *StatsService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

type TrafficBucket = accounting.TrafficBucket
type TrafficSummary = accounting.TrafficSummary

func (s *StatsService) backend() accounting.Recorder {
	return accounting.Recorder{
		Publish: func(current accounting.OnlineResources, stats []model.Stats) {
			publishStatsRealtime(s.projectOnlines(onlines{Inbound: current.Inbound, User: current.User, Outbound: current.Outbound}), stats)
		},
		CommitFailed: func() {
			if err := (&AuditService{Runtime: s.runtime()}).Record(AuditEvent{
				Actor: "system", Event: "stats_commit_failed", Resource: "stats", Severity: AuditSeverityWarn,
				Details: map[string]any{"reason": "commit_failed"},
			}); err != nil {
				logger.Warning("stats commit failure audit failed:", err)
			}
			realtime.Publish(realtime.TopicCoreState, map[string]any{"warning": "stats_commit_failed"})
		},
	}
}

func (s *StatsService) SaveStats(enableTraffic bool) (err error) {
	_, release, err := dbsqlite.AcquireOperation(context.Background())
	if err != nil {
		return err
	}
	defer release()
	coreInstance := s.runtime().Core()
	if coreInstance == nil || !coreInstance.IsRunning() {
		return nil
	}
	_, err = coreInstance.ConsumeStats(func(samples []coretracker.Stat) error {
		return s.saveStatsSamples(enableTraffic, samples)
	})
	return err
}

func (s *StatsService) saveStatsSamples(enableTraffic bool, samples []coretracker.Stat) error {
	observations := make([]accounting.Sample, len(samples))
	for index, sample := range samples {
		observations[index] = accounting.Sample{VPNUserID: sample.VPNUserID, DateTime: sample.DateTime, Resource: sample.Resource, Tag: sample.Tag, Direction: sample.Direction, Traffic: sample.Traffic}
	}
	return s.backend().PersistSamples(enableTraffic, observations)
}

func publishStatsRealtime(current onlines, stats []model.Stats) {
	realtime.Publish(realtime.TopicOnlines, current)
	realtime.Publish(realtime.TopicTrafficDelta, accounting.TrafficDeltas(stats))
}

func (s *StatsService) GetStats(resource, tag string, limit int) ([]model.Stats, error) {
	return s.backend().GetStats(resource, tag, limit)
}
func (s *StatsService) GetInboundTrafficSummary(limit, buckets int, end int64) (TrafficSummary, error) {
	return s.backend().GetInboundTrafficSummary(limit, buckets, end)
}
func (s *StatsService) GetOnlines() (onlines, error) {
	current := accounting.Snapshot()
	return s.projectOnlines(onlines{Inbound: current.Inbound, User: current.User, Outbound: current.Outbound}), nil
}
func (s *StatsService) projectOnlines(current onlines) onlines {
	current.OutboundHealth = s.runtime().Core().OutboundHealthSnapshot()
	return current
}

func (s *StatsService) DelOldStats(days int) error { return s.backend().DelOldStats(days) }
func (s *StatsService) PruneOlderThan(before int64) (int64, error) {
	return s.backend().PruneOlderThan(before)
}
