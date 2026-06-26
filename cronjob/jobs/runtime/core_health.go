package runtime

import (
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type CoreHealthJob struct {
	service.ConfigService
	mu          sync.Mutex
	lastRunning *bool
}

func NewCoreHealthJob() *CoreHealthJob {
	return &CoreHealthJob{}
}

func (s *CoreHealthJob) Run() {
	before := s.ConfigService.IsCoreRunning()
	err := s.ConfigService.StartCore()
	after := s.ConfigService.IsCoreRunning()

	shouldPublish := before != after
	s.mu.Lock()
	if s.lastRunning != nil && *s.lastRunning != after {
		shouldPublish = true
	}
	afterSnapshot := after
	s.lastRunning = &afterSnapshot
	s.mu.Unlock()

	if shouldPublish {
		payload := map[string]any{
			"running": after,
		}
		if err != nil {
			payload["warning"] = "start_failed"
		}
		realtime.Publish(realtime.TopicCoreState, payload)
	}
}
