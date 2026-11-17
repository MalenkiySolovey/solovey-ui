package tokenuse

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
)

func initTokenUseDB(t testing.TB) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "token-use.db")
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
		Resume()
	})
	if err := dbsqlite.InitializeContext(context.Background(), file, dbsqlite.Bootstrap{
		BeforeOpen: func() error { return nil },
		AfterOpen:  func(db *gorm.DB) error { return db.AutoMigrate(&model.User{}, &model.Tokens{}) },
	}); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.User{Id: 1, Username: "fixture"}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRetiredDebouncerCannotWriteThroughCapturedTimerOrNumericIDReuse(t *testing.T) {
	Resume()
	t.Cleanup(Resume)
	writes := 0
	old := New(time.Hour, func(map[uint]Update) error { writes++; return nil })
	old.Record(41, "198.51.100.10", 100)
	epoch := stopTokenUseDebouncerTimer(old)
	finish := BeginReset()
	old.Retire()
	finish()
	Resume()
	old.Record(41, "198.51.100.11", 200)
	old.flushTimer(epoch)
	if err := old.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if writes != 0 || len(old.pending) != 0 {
		t.Fatal("retired database generation regained token write authority")
	}
	current := New(time.Hour, func(updates map[uint]Update) error {
		writes++
		if updates[41] != (Update{IP: "198.51.100.12", At: 300}) {
			t.Fatalf("wrong current generation update: %#v", updates)
		}
		return nil
	})
	t.Cleanup(current.Retire)
	current.Record(41, "198.51.100.12", 300)
	if err := current.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("current generation wrote %d times", writes)
	}
}

func TestFlushUpdatesUsesCanonicalDatabaseAndDoesNotRecreateDeletedTokens(t *testing.T) {
	initTokenUseDB(t)
	updates := make(map[uint]Update)
	rows := make([]model.Tokens, BatchSize+1)
	for i := range rows {
		id := uint(i + 1)
		rows[i] = model.Tokens{Id: id, UserId: 1, TokenHash: "fixture-hash", Enabled: true}
		updates[id] = Update{IP: "198.51.100.20", At: int64(i + 100)}
	}
	if err := dbsqlite.DB().CreateInBatches(&rows, BatchSize).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Delete(&model.Tokens{}, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := FlushUpdates(updates); err != nil {
		t.Fatal(err)
	}
	var stored []model.Tokens
	if err := dbsqlite.DB().Order("id").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != BatchSize {
		t.Fatalf("deleted token was recreated: count=%d", len(stored))
	}
	for _, row := range stored {
		expected := updates[row.Id]
		if row.LastUsedAt != expected.At || row.LastUsedIP != expected.IP {
			t.Fatalf("wrong update for token %d", row.Id)
		}
	}
}
