package operationcoordination

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type transitionRow struct {
	OperationID string `gorm:"primaryKey"`
	Revision    uint64
	State       string
	OwnerFlag   bool
}

type transitionJournal struct {
	ID          uint64 `gorm:"primaryKey"`
	OperationID string
	Revision    uint64
}

func transitionDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "transitions.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&transitionRow{}, &transitionJournal{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestTransitionAtomicJournalAndOwnerFence(t *testing.T) {
	db := transitionDatabase(t)
	initial := transitionRow{OperationID: "one", Revision: 1, State: "PREPARED", OwnerFlag: true}
	if err := db.Create(&initial).Error; err != nil {
		t.Fatal(err)
	}
	conflict := errors.New("owner revision conflict")
	advance := func(ctx context.Context, fence Fence, journal any) error {
		return CommitTransition(ctx, db, &transitionRow{}, fence,
			map[string]any{"revision": fence.Revision + 1, "state": "ACTIVE", "owner_flag": false}, journal, conflict)
	}
	fence := Fence{OperationID: "one", Revision: 1, State: "PREPARED"}
	// Missing owner journal table must not leave the state write committed.
	if err := advance(context.Background(), fence, &struct{ Missing string }{}); err == nil {
		t.Fatal("missing journal was accepted")
	}
	var observed transitionRow
	if err := db.First(&observed, "operation_id = ?", "one").Error; err != nil || observed != initial {
		t.Fatalf("journal failure changed state: %+v %v", observed, err)
	}
	wrongState := fence
	wrongState.State = "OTHER"
	if err := advance(context.Background(), wrongState, &transitionJournal{OperationID: "one", Revision: 2}); !errors.Is(err, conflict) {
		t.Fatalf("state fence: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := advance(ctx, fence, &transitionJournal{OperationID: "one", Revision: 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled pre-write: %v", err)
	}
	if err := advance(context.Background(), fence, &transitionJournal{OperationID: "one", Revision: 2}); err != nil {
		t.Fatal(err)
	}
	if err := advance(context.Background(), fence, &transitionJournal{OperationID: "one", Revision: 2}); !errors.Is(err, conflict) {
		t.Fatalf("stale completion: %v", err)
	}
	if err := db.First(&observed, "operation_id = ?", "one").Error; err != nil || observed.Revision != 2 || observed.OwnerFlag {
		t.Fatalf("owner columns/zero values: %+v %v", observed, err)
	}
	var count int64
	db.Model(&transitionJournal{}).Count(&count)
	if count != 1 {
		t.Fatalf("stale/cancelled transitions wrote journals: %d", count)
	}
}

func TestTransitionConcurrentSameAndIndependentOperations(t *testing.T) {
	db := transitionDatabase(t)
	for _, id := range []string{"same", "independent"} {
		if err := db.Create(&transitionRow{OperationID: id, Revision: 1, State: "PREPARED"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	conflict := errors.New("owner conflict")
	start := make(chan struct{})
	results := make(chan error, 3)
	for _, id := range []string{"same", "same", "independent"} {
		go func(id string) {
			<-start
			results <- CommitTransition(context.Background(), db, &transitionRow{}, Fence{id, 1, "PREPARED"},
				map[string]any{"revision": uint64(2), "state": "ACTIVE"}, &transitionJournal{OperationID: id, Revision: 2}, conflict)
		}(id)
	}
	close(start)
	successes, conflicts := 0, 0
	for range 3 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, conflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 2 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestChangeCorrelationIsOwnerScopedAndCodeOnly(t *testing.T) {
	one := NewReference("update", "panel", "operation:one")
	if one != NewReference("update", "panel", "operation:one") || one.ChangeID == NewReference("data", "database", "operation:one").ChangeID || !one.HostRef.IsLocal() {
		t.Fatal("unstable/cross-owner identity")
	}
	value := Change{Reference: NewReference("update", "panel", "https://example.test/?token=deterministic-private-fixture")}.WithProblem("Bearer deterministic-private-fixture")
	raw, err := json.Marshal(value)
	if err != nil || strings.Contains(string(raw), "deterministic-private-fixture") || value.Problem.Code != "operation_reason_invalid" {
		t.Fatalf("unsafe common projection: %s %v", raw, err)
	}
}
