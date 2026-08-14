//go:build !minimal

package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	observabilitysvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
)

func TestSamplingJobAggregatesBuckets(t *testing.T) {
	initDatabase(t)
	sampler := &coreservice.ObservabilityService{}
	job := NewSamplingJob(sampler)
	tick := 0
	job.currentObservability = func() observabilitysvc.ObservabilitySample {
		value := tick
		tick++
		return observabilitysvc.ObservabilitySample{
			DateTime: int64(value),
			CPU:      float64(value),
			Memory: map[string]interface{}{
				// #nosec G115 -- synthetic test value, always small and non-negative.
				"current": uint64(value),
			},
			Network: map[string]interface{}{
				// #nosec G115 -- synthetic test value, always small and non-negative.
				"sent": uint64(value),
			},
		}
	}
	job.currentCore = func() observabilitysvc.CoreSample {
		return observabilitysvc.CoreSample{
			DateTime: int64(tick),
			Core: map[string]interface{}{
				"tick": tick,
			},
		}
	}
	job.now = func() time.Time {
		return time.Unix(1000+int64(tick), 0)
	}

	for i := 0; i < 30; i++ {
		job.Run()
	}

	samples2s, err := sampler.HistoryForBucket(observabilitysvc.ObservabilityBucket2s)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples2s) < 30 {
		t.Fatalf("expected 30 2s samples, got %d", len(samples2s))
	}
	samples30s, err := sampler.HistoryForBucket(observabilitysvc.ObservabilityBucket30s)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples30s) < 2 {
		t.Fatalf("expected two 30s aggregates, got %d", len(samples30s))
	}
	tail30s := samples30s[len(samples30s)-2:]
	if tail30s[0].CPU != 7 || tail30s[1].CPU != 22 {
		t.Fatalf("unexpected 30s aggregates: %#v", tail30s)
	}
	if tail30s[1].Memory["current"] != float64(22) || tail30s[1].Network["sent"] != float64(22) {
		t.Fatalf("unexpected map aggregates: %#v %#v", tail30s[1].Memory, tail30s[1].Network)
	}

	samples1m, err := sampler.HistoryForBucket(observabilitysvc.ObservabilityBucket1m)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples1m) == 0 {
		t.Fatal("expected one 1m aggregate")
	}
	last1m := samples1m[len(samples1m)-1]
	if last1m.CPU != 14.5 {
		t.Fatalf("unexpected 1m aggregate: %#v", last1m)
	}
	core1m, err := sampler.CoreHistoryForBucket(observabilitysvc.ObservabilityBucket1m)
	if err != nil {
		t.Fatal(err)
	}
	if len(core1m) == 0 {
		t.Fatal("expected one core 1m aggregate")
	}
	lastCore := core1m[len(core1m)-1]
	if lastCore.DateTime != 1030 || lastCore.Core["tick"] != 30 {
		t.Fatalf("unexpected core aggregate: %#v", lastCore)
	}
}

func initDatabase(t *testing.T) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "s-ui-observability-extra-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_DB_FOLDER", tempDir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(tempDir, "s-ui.db")); err != nil {
		removeTempDir(t, tempDir)
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if current := dbsqlite.DB(); current != nil {
			_ = current.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
		}
		_ = dbsqlite.Close()
		ipmonitor.InvalidateAllCache()
		removeTempDir(t, tempDir)
	})
}

func removeTempDir(t *testing.T, dir string) {
	t.Helper()
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		err = os.RemoveAll(dir)
		if err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	t.Errorf("remove observability-extra temp dir %q: %v", dir, err)
}
