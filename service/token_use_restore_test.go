package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestRestoreResetCannotApplyQueuedOrStaleTokenIDsToCandidate(t *testing.T) {
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "token-generation.db")
	if err := dbsqlite.Init(path); err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close(); resumeTokenUseFlush() })
	runtime := NewRuntimeWithCoreProvider(nil)
	old := runtime.tokenUseDebouncer()
	old.Record(41, "198.51.100.10", 100)
	staleEpoch := stopTokenUseDebouncerTimer(old)
	writes := 0
	old.flush = func(updates map[uint]tokenUseUpdate) error { writes++; return flushTokenUseUpdates(updates) }
	owner, err := dbsqlite.BeginMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.End()
	private := owner.Context(context.Background())
	if err := dbsqlite.CloseForFileSwap(private); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.InitContext(private, path); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().WithContext(private).Create(&model.Tokens{Id: 41, Desc: "restored identity", TokenHash: "restore-fixture-hash", UserId: 1, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	runtime.resetTokenUseDebouncerContext(private)
	if writes != 0 {
		t.Fatal("restore reset attempted old-generation numeric-ID writes")
	}
	owner.End()
	resumeTokenUseFlush()
	old.Record(41, "198.51.100.11", 200)
	old.flushTimer(staleEpoch)
	if err := old.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if writes != 0 {
		t.Fatal("stale debouncer regained write authority")
	}
	var restored model.Tokens
	if err := dbsqlite.DB().First(&restored, 41).Error; err != nil || restored.LastUsedAt != 0 || restored.LastUsedIP != "" {
		t.Fatalf("restored token was polluted: %v", err)
	}
	current := runtime.tokenUseDebouncer()
	current.Record(41, "198.51.100.12", 300)
	if err := current.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().First(&restored, 41).Error; err != nil || restored.LastUsedAt != 300 || restored.LastUsedIP != "198.51.100.12" {
		t.Fatalf("new-generation token use failed: %v", err)
	}
}
