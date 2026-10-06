package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/tracker"
)

func TestRecentProbeFreshnessReplacementAndCopiedSnapshot(t *testing.T) {
	var health recentProbeHealth
	now := time.Unix(2000, 0)
	first := health.begin("direct", "https://secret-fixture@target.test/?token=fixture", ProbeSourceManual, now)
	health.complete(first, CheckOutboundResult{OK: true, Delay: 9}, now)
	fresh := health.snapshot(now.Add(time.Second))
	if fresh["direct"].Source != ProbeSourceManual || fresh["direct"].DelayMs != 9 || fresh["direct"].Status != "healthy" {
		t.Fatalf("fresh = %#v", fresh)
	}
	encoded, err := json.Marshal(fresh)
	if err != nil || strings.Contains(string(encoded), "secret-fixture") || strings.Contains(string(encoded), "target.test") {
		t.Fatalf("unsafe snapshot: %s / %v", encoded, err)
	}
	delete(fresh, "direct")
	if len(health.snapshot(now)) != 1 {
		t.Fatal("snapshot mutation reached owner")
	}
	older := health.begin("direct", "older", ProbeSourceManual, now.Add(time.Second))
	newer := health.begin("direct", "newer", ProbeSourceFailover, now.Add(2*time.Second))
	health.complete(newer, CheckOutboundResult{Error: CheckOutboundErrorTimeout}, now.Add(3*time.Second))
	health.complete(older, CheckOutboundResult{OK: true}, now.Add(4*time.Second))
	got := health.snapshot(now.Add(5 * time.Second))["direct"]
	if got.Source != ProbeSourceFailover || got.Status != "down" || got.Error != CheckOutboundErrorTimeout {
		t.Fatalf("obsolete completion overwrote result: %#v", got)
	}
	if len(health.snapshot(now.Add(3*time.Second+probeHealthMaxAge))) != 0 {
		t.Fatal("expired success/failure projected as current")
	}
	if len(health.entries) != 1 {
		t.Fatal("snapshot pruned owner state")
	}
	unknown := health.begin("direct", "newer", ProbeSourceDiagnostic, now.Add(5*time.Second))
	health.complete(unknown, CheckOutboundResult{Error: CheckOutboundErrorCanceled}, now.Add(6*time.Second))
	if health.snapshot(now.Add(6 * time.Second))["direct"].Status != "unknown" {
		t.Fatal("cancellation falsely proves a target is down")
	}
}

func TestHealthSnapshotInsideStatsLeaseWithStopWaiting(t *testing.T) {
	core := NewCore()
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	ticket := core.probeHealth.begin("direct", "target", ProbeSourceManual, time.Now())
	core.probeHealth.complete(ticket, CheckOutboundResult{OK: true}, time.Now())
	stopped := make(chan error, 1)
	available, err := core.ConsumeStats(func(_ []tracker.Stat) error {
		go func() { stopped <- core.Stop() }()
		deadline := time.Now().Add(5 * time.Second)
		for core.lifecycle.TryRLock() {
			core.lifecycle.RUnlock()
			if time.Now().After(deadline) {
				return fmt.Errorf("fixture Stop did not queue")
			}
			goruntime.Gosched()
		}
		if len(core.OutboundHealthSnapshot()) != 1 {
			return fmt.Errorf("leased observation lost")
		}
		return nil
	})
	if stopErr := <-stopped; stopErr != nil {
		t.Fatal(stopErr)
	}
	if !available || err != nil {
		t.Fatalf("stats lease = %v / %v", available, err)
	}
	if len(core.OutboundHealthSnapshot()) != 0 {
		t.Fatal("stop retained current health")
	}
}

func TestRecentProbeRetentionRemovalAndRestartFence(t *testing.T) {
	var health recentProbeHealth
	now := time.Unix(2000, 0)
	for i := 0; i < probeHealthMaxEntries+5; i++ {
		ticket := health.begin(fmt.Sprintf("tag-%04d", i), "target", ProbeSourceManual, now)
		health.complete(ticket, CheckOutboundResult{OK: true}, now)
	}
	if len(health.entries) != probeHealthMaxEntries || len(health.snapshot(now)) != probeHealthMaxEntries {
		t.Fatal("retention is not bounded")
	}
	if _, found := health.snapshot(now)["tag-0000"]; found {
		t.Fatal("oldest entry not evicted deterministically")
	}
	old := health.begin("deleted", "target", ProbeSourceManual, now)
	health.remove("deleted")
	health.complete(old, CheckOutboundResult{OK: true}, now)
	if _, found := health.snapshot(now)["deleted"]; found {
		t.Fatal("deleted target resurrected by old probe")
	}
	beforeRestart := health.begin("direct", "target", ProbeSourceManual, now)
	health.reset()
	afterRestart := health.begin("direct", "target", ProbeSourceFailover, now)
	health.complete(beforeRestart, CheckOutboundResult{OK: true}, now)
	if len(health.snapshot(now)) != 0 {
		t.Fatal("prior runtime result resurrected")
	}
	health.complete(afterRestart, CheckOutboundResult{Error: "password=synthetic-raw-error"}, now)
	if health.snapshot(now)["direct"].Error != CheckOutboundErrorFailed {
		t.Fatal("raw probe error escaped")
	}
	health.begin("new", "target", ProbeSourceManual, now.Add(probeHealthMaxAge))
	if len(health.entries) != 1 {
		t.Fatal("writes retain expired or deleted targets forever")
	}
}

func TestRecentProbeConcurrentUpdatesAndSnapshots(t *testing.T) {
	var health recentProbeHealth
	now := time.Unix(2000, 0)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			<-start
			for n := 0; n < 100; n++ {
				tag := fmt.Sprintf("tag-%d", worker)
				ticket := health.begin(tag, "target", ProbeSourceManual, now)
				health.complete(ticket, CheckOutboundResult{OK: true}, now)
				copy := health.snapshot(now)
				delete(copy, tag)
				if n%10 == 0 {
					health.remove(tag)
				}
				if worker == 0 && n%25 == 0 {
					health.reset()
				}
			}
		}(i)
	}
	close(start)
	workers.Wait()
}

func TestCoreProbeHealthActualRuntimeAndTargetLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	core := NewCore()
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := core.Stop(); err != nil {
			t.Error(err)
		}
	})
	for _, source := range []ProbeSource{ProbeSourceManual, ProbeSourceFailover, ProbeSourceDiagnostic} {
		result := core.CheckOutboundWithSource(context.Background(), "direct", server.URL, source)
		if !result.OK {
			t.Fatalf("probe failed: %#v", result)
		}
		if core.OutboundHealthSnapshot()["direct"].Source != source {
			t.Fatal("probe source lost")
		}
	}
	if err := core.RemoveOutbound("direct"); err != nil {
		t.Fatal(err)
	}
	if len(core.OutboundHealthSnapshot()) != 0 {
		t.Fatal("deleted runtime target remains")
	}
	if err := core.AddOutbound([]byte(`{"type":"direct","tag":"direct"}`)); err != nil {
		t.Fatal(err)
	}
	if len(core.OutboundHealthSnapshot()) != 0 {
		t.Fatal("replacement inherited old result")
	}
	if err := core.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(core.OutboundHealthSnapshot()) != 0 {
		t.Fatal("stopped runtime health is current")
	}
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	if len(core.OutboundHealthSnapshot()) != 0 {
		t.Fatal("restart inherited historical success")
	}
}

func TestCoreProbeCompletionAfterTargetReplacementIsDiscarded(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	core := NewCore()
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := core.Stop(); err != nil {
			t.Error(err)
		}
	})
	done := make(chan CheckOutboundResult, 1)
	go func() { done <- core.CheckOutbound(context.Background(), "direct", server.URL) }()
	<-entered
	if err := core.RemoveOutbound("direct"); err != nil {
		close(release)
		t.Fatal(err)
	}
	if err := core.AddOutbound([]byte(`{"type":"direct","tag":"direct"}`)); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if result := <-done; !result.OK {
		t.Fatalf("fixture probe = %#v", result)
	}
	if len(core.OutboundHealthSnapshot()) != 0 {
		t.Fatal("in-flight old target success became replacement health")
	}
}
