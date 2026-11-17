package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/restorestate"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func restoreFixture(t *testing.T) (Restorer, []byte, string, *int) {
	t.Helper()
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	open := func(ctx context.Context, path string) error {
		return dbsqlite.InitializeContext(ctx, path, dbsqlite.Bootstrap{
			BeforeOpen: func() error { return nil },
			AfterOpen:  func(db *gorm.DB) error { return db.AutoMigrate(&model.Setting{}) },
		})
	}
	if err := open(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: "restore_marker", Value: "live"}).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := func(name, value string) []byte {
		t.Helper()
		path := filepath.Join(root, name)
		db, err := gorm.Open(gormsqlite.Open(path), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		if err := db.AutoMigrate(&model.Setting{}); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.Setting{Key: "restore_marker", Value: value}).Error; err != nil {
			t.Fatal(err)
		}
		if err := raw.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	candidate := snapshot("candidate.db", "candidate")
	snapshot("recovery.db", "live")
	restarts := 0
	r := Restorer{
		DatabasePath: func() string { return live }, OpenDatabase: open,
		Rehearse: func(ctx context.Context, source io.ReadSeeker) (RestoreRehearsal, error) {
			data, err := io.ReadAll(source)
			if err != nil {
				return RestoreRehearsal{}, err
			}
			if _, err := source.Seek(0, io.SeekStart); err != nil {
				return RestoreRehearsal{}, err
			}
			digest := sha256.Sum256(data)
			return RestoreRehearsal{Possible: true, BackupDigest: hex.EncodeToString(digest[:]), Manifest: &BackupManifest{}}, ctx.Err()
		},
		ValidateBackup: func(path string) error {
			db, err := dbsqlite.OpenReadOnly(context.Background(), path)
			if err != nil {
				return err
			}
			raw, err := db.DB()
			if err != nil {
				return err
			}
			defer raw.Close()
			var value string
			if err := db.Raw("PRAGMA integrity_check").Scan(&value).Error; err != nil {
				return err
			}
			if value != "ok" {
				return errors.New("invalid candidate integrity")
			}
			return nil
		},
		PrepareExport: func(context.Context, string) (string, func(), error) {
			return filepath.Join(root, "recovery.db"), func() {}, nil
		},
		MigrateOwners: func(context.Context, string, []RestoreOwnerStatus, ...*FileBackupManifest) ([]RestoreOwnerStatus, error) {
			return nil, nil
		},
		NormalizeOwners:   func(context.Context, *gorm.DB, []RestoreOwnerStatus) ([]RestoreOwnerStatus, error) { return nil, nil },
		RestoreOwnerFiles: func(context.Context, *gorm.DB, *FileBackupManifest, bool) error { return nil },
		Restart:           func() error { restarts++; return nil },
	}
	t.Cleanup(func() {
		if pendingMaintenance() != nil {
			if err := r.Abort(); err != nil {
				t.Error(err)
			}
		}
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	return r, candidate, filepath.Join(root, "recovery", "restore"), &restarts
}

func restoreMarker(t *testing.T, ctx context.Context) string {
	t.Helper()
	var value string
	if err := dbsqlite.DB().WithContext(ctx).Model(&model.Setting{}).Select("value").Where("key = ?", "restore_marker").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRestoreRetainsPrivateAdmissionUntilAuthorityCommit(t *testing.T) {
	r, candidate, root, restarts := restoreFixture(t)
	result, err := r.Detailed(context.Background(), bytes.NewReader(candidate), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RecoveryBackupRef) != 64 {
		t.Fatalf("recovery reference=%q", result.RecoveryBackupRef)
	}
	if _, _, err := dbsqlite.AcquireOperation(context.Background()); !errors.Is(err, dbsqlite.ErrMaintenance) {
		t.Fatalf("candidate published before authority commit: %v", err)
	}
	ctx := result.DatabaseContext(context.Background())
	if got := restoreMarker(t, ctx); got != "candidate" {
		t.Fatalf("private marker=%q", got)
	}
	if err := dbsqlite.DB().WithContext(ctx).Create(&model.Setting{Key: "operation_authority", Value: "committed"}).Error; err != nil {
		t.Fatal(err)
	}
	cleanup, restart, err := r.Complete(context.Background())
	if err != nil || cleanup || restart || *restarts != 1 {
		t.Fatalf("accept cleanup=%v restart=%v calls=%d err=%v", cleanup, restart, *restarts, err)
	}
	if got := restoreMarker(t, context.Background()); got != "candidate" {
		t.Fatalf("published marker=%q", got)
	}
	var count int64
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ? AND value = ?", "operation_authority", "committed").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("authority count=%d err=%v", count, err)
	}
	for _, path := range []string{restorestate.MarkerPath(r.DatabasePath()), restorestate.FallbackPath(r.DatabasePath())} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("accepted cleanup %s: %v", path, err)
		}
	}
}

func TestRestoreAbortReopensExactFallbackWithoutRestart(t *testing.T) {
	r, candidate, root, restarts := restoreFixture(t)
	if _, err := r.Detailed(context.Background(), bytes.NewReader(candidate), root); err != nil {
		t.Fatal(err)
	}
	if err := r.Abort(); err != nil {
		t.Fatal(err)
	}
	if got := restoreMarker(t, context.Background()); got != "live" {
		t.Fatalf("fallback marker=%q", got)
	}
	if *restarts != 0 || pendingMaintenance() != nil {
		t.Fatal("aborted restore restarted or retained maintenance")
	}
}

func TestRestoreCanceledPostOpenUsesIndependentRecoveryLifetime(t *testing.T) {
	r, candidate, root, restarts := restoreFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.NormalizeOwners = func(ctx context.Context, _ *gorm.DB, _ []RestoreOwnerStatus) ([]RestoreOwnerStatus, error) {
		cancel()
		return nil, ctx.Err()
	}
	if _, err := r.Detailed(ctx, bytes.NewReader(candidate), root); err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("canceled restore=%v", err)
	}
	if got := restoreMarker(t, context.Background()); got != "live" {
		t.Fatalf("recovery marker=%q", got)
	}
	if *restarts != 0 || pendingMaintenance() != nil {
		t.Fatal("rejected restore restarted or retained maintenance")
	}
	_, release, err := dbsqlite.AcquireOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestRestoreRejectedRehearsalHasNoDatabaseOrRecoveryEffects(t *testing.T) {
	r, candidate, root, restarts := restoreFixture(t)
	r.Rehearse = func(context.Context, io.ReadSeeker) (RestoreRehearsal, error) {
		return RestoreRehearsal{Possible: false, ReasonCodes: []string{"OWNER_REJECTED"}}, nil
	}
	if _, err := r.Detailed(context.Background(), bytes.NewReader(candidate), root); err == nil || !strings.Contains(err.Error(), "OWNER_REJECTED") {
		t.Fatalf("rehearsal rejection=%v", err)
	}
	if got := restoreMarker(t, context.Background()); got != "live" {
		t.Fatalf("rejected marker=%q", got)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected recovery root=%v", err)
	}
	if *restarts != 0 || pendingMaintenance() != nil {
		t.Fatal("rejected rehearsal acquired restore authority")
	}
}
