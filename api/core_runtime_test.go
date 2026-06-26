package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type restartSchedulerRecorder struct {
	delay time.Duration
}

func (s *restartSchedulerRecorder) ScheduleRestart(delay time.Duration) error {
	s.delay = delay
	return nil
}

func TestRestartAppUsesInjectedScheduler(t *testing.T) {
	recorder := &restartSchedulerRecorder{}
	service := ApiService{RestartScheduler: recorder}
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)

	service.configHandler().RestartApp(context)

	if recorder.delay != 3*time.Second {
		t.Fatalf("scheduled delay = %s, want 3s", recorder.delay)
	}
}
