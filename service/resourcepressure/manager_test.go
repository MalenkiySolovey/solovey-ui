package resourcepressure

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/ops/resourcepressure"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fixtureCollector struct {
	value float64
}

func (collector *fixtureCollector) Collect(_ context.Context, now time.Time) []domain.Signal {
	return []domain.Signal{{ID: "fixture.used_ratio", Status: domain.ProviderSupported, Value: collector.value, Unit: "ratio",
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}}
}

type overlapDetectingCollector struct {
	active    atomic.Int32
	maxActive atomic.Int32
}

func (collector *overlapDetectingCollector) Collect(_ context.Context, now time.Time) []domain.Signal {
	active := collector.active.Add(1)
	for {
		maximum := collector.maxActive.Load()
		if active <= maximum || collector.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	collector.active.Add(-1)
	return []domain.Signal{{ID: "fixture.used_ratio", Status: domain.ProviderSupported, Value: .2, Unit: "ratio",
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}}
}

func TestManagerPersistsOnlyTransitionsAndDistrustsRestoredPressure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pressure.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.ResourcePressureState{}, &model.ResourcePressureTransition{}); err != nil {
		t.Fatal(err)
	}
	evaluator, err := domain.NewEvaluator([]domain.Threshold{{ID: "fixture.used_ratio", Direction: domain.HigherIsWorse,
		Warning: .8, Constrained: .9, Critical: .96, Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	collector := &fixtureCollector{value: .2}
	manager := NewManager(evaluator, collector, Repository{DB: func() *gorm.DB { return db }})
	manager.now = func() time.Time { return now }
	if err := manager.Observe(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(domain.SampleInterval)
	if err := manager.Observe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := manager.Current(); got.State != domain.StateNormal {
		t.Fatalf("two samples did not establish normal pressure: %#v", got)
	}
	now = now.Add(domain.SampleInterval)
	if err := manager.Observe(context.Background()); err != nil {
		t.Fatal(err)
	}
	var transitions int64
	if err := db.Model(&model.ResourcePressureTransition{}).Count(&transitions).Error; err != nil || transitions != 1 {
		t.Fatalf("transition persistence count=%d err=%v", transitions, err)
	}
	var rows int64
	if err := db.Model(&model.ResourcePressureState{}).Count(&rows).Error; err != nil || rows != 1 {
		t.Fatalf("bounded pressure aggregate rows=%d err=%v", rows, err)
	}

	restartedEvaluator, _ := domain.NewEvaluator([]domain.Threshold{{ID: "fixture.used_ratio", Direction: domain.HigherIsWorse,
		Warning: .8, Constrained: .9, Critical: .96, Required: true}})
	restarted := NewManager(restartedEvaluator, collector, Repository{DB: func() *gorm.DB { return db }})
	if got := restarted.Current(); got.State != domain.StateUnknown || got.ReasonCodes[0] != "pressure_not_observed" {
		t.Fatalf("restored durable pressure became current without fresh samples: %#v", got)
	}
}

func TestManagerSerializesObservationCycles(t *testing.T) {
	evaluator, err := domain.NewEvaluator([]domain.Threshold{{ID: "fixture.used_ratio", Direction: domain.HigherIsWorse,
		Warning: .8, Constrained: .9, Critical: .96, Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	collector := &overlapDetectingCollector{}
	manager := NewManager(evaluator, collector, Repository{})
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			if err := manager.Observe(context.Background()); err != nil {
				t.Errorf("Observe() error = %v", err)
			}
		}()
	}
	workers.Wait()
	if maximum := collector.maxActive.Load(); maximum != 1 {
		t.Fatalf("observation cycles overlapped: max active = %d", maximum)
	}
}

func TestManagerPublishesSnapshotOnlyAfterDurableWrite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pressure-retry.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	evaluator, err := domain.NewEvaluator([]domain.Threshold{{ID: "fixture.used_ratio", Direction: domain.HigherIsWorse,
		Warning: .8, Constrained: .9, Critical: .96, Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	manager := NewManager(evaluator, &fixtureCollector{value: .2}, Repository{DB: func() *gorm.DB { return db }})
	manager.now = func() time.Time { return now }

	if err := manager.Observe(context.Background()); err == nil {
		t.Fatal("Observe succeeded without durable pressure tables")
	}
	if got := manager.Current(); got.Revision != 0 || got.ReasonCodes[0] != "pressure_not_observed" {
		t.Fatalf("failed persistence published snapshot: %#v", got)
	}
	if err := db.AutoMigrate(&model.ResourcePressureState{}, &model.ResourcePressureTransition{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(domain.SampleInterval)
	if err := manager.Observe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := manager.Current(); got.Revision == 0 {
		t.Fatalf("successful retry did not publish snapshot: %#v", got)
	}
}

func TestManagerCanRestartAfterStop(t *testing.T) {
	evaluator, err := domain.NewEvaluator([]domain.Threshold{{ID: "fixture.used_ratio", Direction: domain.HigherIsWorse,
		Warning: .8, Constrained: .9, Critical: .96, Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(evaluator, &fixtureCollector{value: .2}, Repository{})
	manager.Start(context.Background())
	manager.mu.RLock()
	firstDone := manager.done
	manager.mu.RUnlock()
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	manager.Start(context.Background())
	manager.mu.RLock()
	secondDone := manager.done
	manager.mu.RUnlock()
	if secondDone == nil || secondDone == firstDone {
		t.Fatal("Start after Stop did not create a new lifecycle generation")
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSystemCollectorInventoryIsBoundedUniqueAndExplicit(t *testing.T) {
	now := time.Now().UTC()
	signals := (SystemCollector{}).Collect(context.Background(), now)
	if len(signals) == 0 || len(signals) > domain.MaxSignals {
		t.Fatalf("system signal inventory size=%d", len(signals))
	}
	seen := map[string]domain.Signal{}
	for _, signal := range signals {
		if _, duplicate := seen[signal.ID]; duplicate {
			t.Fatalf("system signal %q is duplicated", signal.ID)
		}
		if signal.Status == "" || signal.ObservedAt != now.Unix() || signal.ExpiresAt <= signal.ObservedAt {
			t.Fatalf("system signal lacks typed state/freshness: %#v", signal)
		}
		seen[signal.ID] = signal
	}
	for _, id := range []string{
		"filesystem.data.free_ratio", "filesystem.data.free_bytes", "filesystem.data.free_inode_ratio",
		"filesystem.temp.free_ratio", "filesystem.temp.free_bytes", "filesystem.temp.free_inode_ratio",
		"memory.used_ratio", "sqlite.wal.bytes", "sqlite.busy.rate", "process.goroutines",
		"process.fd.count", "process.fd.used_ratio", "cgroup.memory.used_ratio", "psi.memory.some_avg10",
		"http.active", "audit.queue.used_ratio", "operations.heavy.active",
	} {
		if _, ok := seen[id]; !ok {
			t.Errorf("system provider %q is absent", id)
		}
	}
}

func TestBoundedTempFilesystemUsesRatioWithoutPermanentCriticalState(t *testing.T) {
	const total = uint64(64 << 20)
	const free = uint64(60 << 20)
	bytesSignal := boundedFilesystemFreeBytesSignal(
		"filesystem.temp.free_bytes", "filesystem_temp_absolute_bytes_inapplicable", total, free,
	)
	if bytesSignal.Status != domain.ProviderUnsupported || bytesSignal.ReasonCode != "filesystem_temp_absolute_bytes_inapplicable" {
		t.Fatalf("bounded tmpfs absolute provider=%#v", bytesSignal)
	}
	largeSignal := boundedFilesystemFreeBytesSignal(
		"filesystem.temp.free_bytes", "filesystem_temp_absolute_bytes_inapplicable", 4<<30, 3<<30,
	)
	if largeSignal.Status != domain.ProviderSupported || largeSignal.Value != float64(3<<30) {
		t.Fatalf("ordinary temp filesystem absolute provider=%#v", largeSignal)
	}

	evaluator, err := domain.NewEvaluator([]domain.Threshold{
		{ID: "filesystem.temp.free_ratio", Direction: domain.LowerIsWorse, Warning: .20, Constrained: .10, Critical: .05},
		{ID: "filesystem.temp.free_bytes", Direction: domain.LowerIsWorse, Warning: 2 << 30, Constrained: 1 << 30, Critical: 512 << 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	var observed domain.Snapshot
	for range 2 {
		bytesSignal.ObservedAt, bytesSignal.ExpiresAt = now.Unix(), now.Add(domain.DefaultFreshness).Unix()
		ratioSignal := domain.Signal{ID: "filesystem.temp.free_ratio", Status: domain.ProviderSupported,
			Value: float64(free) / float64(total), Unit: "ratio", ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.DefaultFreshness).Unix()}
		observed = evaluator.Evaluate(now, []domain.Signal{ratioSignal, bytesSignal})
		if observed.State == domain.StateCritical {
			t.Fatalf("bounded healthy tmpfs became critical: %#v", observed)
		}
		now = now.Add(domain.SampleInterval)
	}
	if observed.State != domain.StateNormal {
		t.Fatalf("bounded healthy tmpfs did not establish normal pressure: %#v", observed)
	}
}

func TestPersistedTransitionRetentionKeepsNewestAuthority(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:pressure-retention?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.ResourcePressureTransition{}); err != nil {
		t.Fatal(err)
	}
	rows := make([]model.ResourcePressureTransition, MaxPersistedTransitions+37)
	for index := range rows {
		rows[index] = model.ResourcePressureTransition{FromState: "NORMAL", ToState: "WARNING",
			ReasonCode: "fixture", ObservationDigest: pressureDigest(index), Revision: uint64(index + 1), CreatedAt: int64(index + 1)}
	}
	if err := db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(prunePersistedTransitions); err != nil {
		t.Fatal(err)
	}
	var retained []model.ResourcePressureTransition
	if err := db.Order("sequence ASC").Find(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if len(retained) != MaxPersistedTransitions || retained[0].Sequence != 38 ||
		retained[len(retained)-1].Sequence != uint64(len(rows)) {
		t.Fatalf("retention=%d first=%d last=%d", len(retained), retained[0].Sequence, retained[len(retained)-1].Sequence)
	}
}

func pressureDigest(index int) string {
	return fmt.Sprintf("%064x", index+1)
}
