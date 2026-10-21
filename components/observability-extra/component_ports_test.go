//go:build !minimal

package observabilityextra

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	observationsvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

type injectedSamplingScheduler struct {
	observabilityTrackingScheduler
	job cron.Job
}

func (s *injectedSamplingScheduler) AddJob(_ string, job cron.Job) (cron.EntryID, error) {
	s.job = job
	s.added++
	return cron.EntryID(s.added), nil
}

type injectedHistory struct {
	observationsvc.History
	reads int
}

func (h *injectedHistory) HistoryForBucketSince(observationsvc.ObservabilityBucket, int64) ([]observationsvc.ObservabilitySample, error) {
	h.reads++
	return []observationsvc.ObservabilitySample{}, nil
}

type injectedAuditHistory struct{ reads int }

func (h *injectedAuditHistory) ListPageFiltered(uint64, int, string, string, int64, int64) ([]model.AuditEvent, uint64, error) {
	h.reads++
	return []model.AuditEvent{}, 0, nil
}

func TestRegisteredComponentUsesInjectedSamplingAndAuthorizedHistoryOwners(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sampler, history, audit := &trackingSampler{}, &injectedHistory{}, &injectedAuditHistory{}
	scheduler := &injectedSamplingScheduler{}
	allowed := true
	require := func(c *gin.Context) bool {
		if !allowed {
			c.AbortWithStatus(http.StatusForbidden)
		}
		return allowed
	}
	api := componenthost.APIDeps{
		ObservationPorts: componenthost.ObservationPorts{ObservationSampler: sampler, ObservationHistory: history, AuditHistory: audit},
		Auth: componenthost.AuthDeps{
			RequireScope:           func(c *gin.Context, _ string, _ ...string) bool { return require(c) },
			RequireAuditAdminScope: require,
		},
		Request: componenthost.RequestDeps{Actor: func(*gin.Context) string { return "test-admin" }, RemoteIP: func(*gin.Context) string { return "192.0.2.1" }},
		Rate:    componenthost.RateLimitDeps{CheckAuditRateLimit: func(string) error { return nil }, AuditRateLimitKey: func(string, string) string { return "test" }, AuditRateLimitWindow: time.Minute},
		HTTP: componenthost.HTTPDeps{JSONObj: func(c *gin.Context, obj interface{}, err error) {
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": false})
				return
			}
			c.JSON(http.StatusOK, obj)
		}},
	}
	registered, ok := registry.ComponentByID(id)
	if !ok || registered.Manifest.HostAPI == nil {
		t.Fatal("typed component is not registered")
	}
	host := lifecycle.Context{Host: componenthost.Deps{API: api, Scheduler: scheduler}}
	if err := registered.Lifecycle.Start(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := registered.Lifecycle.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	scheduler.job.Run()
	if sampler.observations != 1 || sampler.cores != 1 {
		t.Fatalf("injected sampling owner not used: %+v", sampler)
	}
	router := gin.New()
	routes := registry.APIRouteRegistrarsByComponentIDs(map[string]struct{}{id: {}})
	if len(routes) != 1 {
		t.Fatalf("routes: %d", len(routes))
	}
	if err := routes[0].Register(router.Group("/api"), api); err != nil {
		t.Fatal(err)
	}
	request := func(path string) int {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder.Code
	}
	for _, path := range []string{"/api/observability/history?bucket=2s", "/api/security/audit"} {
		if status := request(path); status != http.StatusOK {
			t.Fatalf("authorized %s: %d", path, status)
		}
	}
	allowed = false
	for _, path := range []string{"/api/observability/history?bucket=2s", "/api/security/audit"} {
		if status := request(path); status != http.StatusForbidden {
			t.Fatalf("denied %s: %d", path, status)
		}
	}
	if history.reads != 1 || audit.reads != 1 {
		t.Fatalf("denied request reached owner: history=%d audit=%d", history.reads, audit.reads)
	}
}

func TestMissingSamplingOwnerDoesNotScheduleWork(t *testing.T) {
	scheduler := &observabilityTrackingScheduler{}
	c := &component{}
	if err := c.Start(context.Background(), componenthost.Ports{Scheduler: scheduler}); err == nil || scheduler.added != 0 || c.started {
		t.Fatalf("unavailable sampling owner scheduled work: %v %+v", err, c)
	}
}
