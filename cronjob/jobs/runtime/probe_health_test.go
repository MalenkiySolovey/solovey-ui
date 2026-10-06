package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestFailoverAdapterRecordsProbeInRuntimeOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	core := coreruntime.NewCore()
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := core.Stop(); err != nil {
			t.Error(err)
		}
	})
	job := NewFailoverJob(context.Background())
	job.ConfigService.Runtime = service.NewRuntime(core)
	results := job.probeMembers(entityoutbounds.FailoverGroup{Members: []string{"direct"}, ProbeTarget: server.URL, Interval: time.Minute})
	health := core.OutboundHealthSnapshot()["direct"]
	if !results["direct"] || health.Source != coreruntime.ProbeSourceFailover || health.Status != "healthy" || health.Tag != "direct" {
		t.Fatalf("failover health lost: %#v / %#v", results, health)
	}
}
