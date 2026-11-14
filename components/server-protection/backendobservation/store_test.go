package backendobservation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/observationio"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestObservationCycleRetainsProjectionAndClosesBeforeFurtherWrites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "observations.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	accepted := repository.ProfileModel{ResourceID: "owned:surface", Status: "active", Enabled: true, Mode: "record_only", AcceptedFingerprint: "accepted",
		ScoreThreshold: 50, GraylistTTLSeconds: 120, Revision: 3}
	if err := db.Create(&accepted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repository.ProfileModel{ResourceID: "other:surface", Status: "stale", Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	owner := NewStore(repository.New(db))
	cycle, err := owner.BeginObservation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := cycle.ActiveProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].ResourceID != accepted.ResourceID || !profiles[0].Enabled || profiles[0].Mode != accepted.Mode ||
		profiles[0].AcceptedFingerprint != accepted.AcceptedFingerprint || profiles[0].ScoreThreshold != accepted.ScoreThreshold || profiles[0].GraylistTTLSeconds != accepted.GraylistTTLSeconds {
		t.Fatalf("existing active profile projection changed: %#v", profiles)
	}
	cycle.Close()
	cycle.Close()
	if _, err := cycle.ActiveProfiles(); !errors.Is(err, observationio.ErrCycleClosed) {
		t.Fatalf("closed read: %v", err)
	}
	if err := cycle.PersistObservationBatch(nil, nil, nil); !errors.Is(err, observationio.ErrCycleClosed) {
		t.Fatalf("closed write: %v", err)
	}
	var count int64
	if err := db.Model(&repository.ProbeEventModel{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("closed cycle wrote events: %d %v", count, err)
	}
}

func TestObservationCycleCancellationAndMissingOwnerFailClosed(t *testing.T) {
	if NewStore(nil) != nil {
		t.Fatal("missing owner became a store")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cycle, err := NewStore(repository.New(nil)).BeginObservation(ctx)
	if cycle != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission: %#v %v", cycle, err)
	}
}

func TestObservationCycleRetainsAndReleasesActualBackendGenerationAdmission(t *testing.T) {
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "admission.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	cycle, err := NewStore(repository.New(dbsqlite.DB())).BeginObservation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer cycle.Close()
	admitted := cycle.Context()
	nested, release, err := dbsqlite.AcquireOperation(admitted)
	if err != nil || nested != admitted {
		t.Fatalf("batch lost its Backend admission: nested=%v err=%v", nested == admitted, err)
	}
	release()
	cycle.Close()
	if _, release, err := dbsqlite.AcquireOperation(admitted); !errors.Is(err, dbsqlite.ErrRetired) {
		if release != nil {
			release()
		}
		t.Fatalf("closed batch context retained generation authority: %v", err)
	}
}

func TestObservationBusyClassificationKeepsDriverDiagnosticsWithBackend(t *testing.T) {
	for _, message := range []string{"database is locked", "DATABASE IS BUSY", "SQLITE_BUSY: retry"} {
		cause := errors.New(message)
		err := observationWriteError(cause)
		if !errors.Is(err, observationio.ErrWriteBusy) || !errors.Is(err, cause) || err.Error() != message {
			t.Fatalf("busy classification changed diagnostic or cause: %v", err)
		}
	}
	cause := errors.New("permission denied")
	if observationWriteError(cause) != cause || observationWriteError(nil) != nil {
		t.Fatal("unrelated failures became retryable")
	}
}
