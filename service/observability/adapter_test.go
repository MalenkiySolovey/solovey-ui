//go:build !minimal

package observability

import (
	"context"
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"path/filepath"
	"testing"
)

type adapterSettings struct{}

func (adapterSettings) GetObservabilityMemoryCapMB() (int, error) { return 32, nil }
func isolateHistory(t *testing.T) {
	t.Helper()
	sharedHistory.Lock()
	old, db := sharedHistory.store, sharedHistory.database
	sharedHistory.store = nil
	sharedHistory.database = nil
	sharedHistory.Unlock()
	t.Cleanup(func() {
		sharedHistory.Lock()
		sharedHistory.store = old
		sharedHistory.database = db
		sharedHistory.Unlock()
	})
}
func TestInactiveHistoryAndResetDoNotAllocateStore(t *testing.T) {
	isolateHistory(t)
	resetHistory()
	if sharedHistory.store != nil {
		t.Fatal("inactive reset allocated optional history")
	}
}
func TestDatabaseResetHookExistsOnlyForItsActiveBinding(t *testing.T) {
	isolateHistory(t)
	owner := &Service{Store: SharedStore(), Settings: adapterSettings{}}
	unbind := BindDatabaseReset()
	t.Cleanup(unbind)
	_ = owner.RecordObservabilitySample(ObservabilityBucket2s, ObservabilitySample{DateTime: 1})
	if err := dbhooks.ResetCaches(context.Background()); err != nil {
		t.Fatal(err)
	}
	samples, _ := owner.HistoryForBucket(ObservabilityBucket2s)
	if len(samples) != 0 {
		t.Fatal("active reset retained history")
	}
	unbind()
	_ = owner.RecordObservabilitySample(ObservabilityBucket2s, ObservabilitySample{DateTime: 2})
	if err := dbhooks.ResetCaches(context.Background()); err != nil {
		t.Fatal(err)
	}
	samples, _ = owner.HistoryForBucket(ObservabilityBucket2s)
	if len(samples) != 1 {
		t.Fatal("disabled reset hook remained active")
	}
}
func TestDatabaseReplacementWhileInactiveInvalidatesHistory(t *testing.T) {
	isolateHistory(t)
	_ = dbsqlite.Close()
	first := filepath.Join(t.TempDir(), "first.db")
	second := filepath.Join(t.TempDir(), "second.db")
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if err := dbsqlite.Init(first); err != nil {
		t.Fatal(err)
	}
	owner := &Service{Store: SharedStore(), Settings: adapterSettings{}}
	_ = owner.RecordObservabilitySample(ObservabilityBucket2s, ObservabilitySample{DateTime: 1})
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(second); err != nil {
		t.Fatal(err)
	}
	next := &Service{Store: SharedStore(), Settings: adapterSettings{}}
	samples, _ := next.HistoryForBucket(ObservabilityBucket2s)
	if len(samples) != 0 {
		t.Fatal("inactive database replacement retained stale history")
	}
}
