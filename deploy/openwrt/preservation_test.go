package openwrt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
)

type preservationFixture struct {
	databasePath string
	root         string
	metadata     PreservationMetadataV1
}

func TestLogicalPreservationUsesRehearsedSnapshotDuringWALActivity(t *testing.T) {
	fixture := newPreservationFixture(t)
	if _, err := os.Lstat(fixture.databasePath + "-wal"); err != nil {
		t.Fatalf("live WAL activity was not established: %v", err)
	}
	metadata, rehearsal, err := LoadSysupgradePreservation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Identity != fixture.metadata.Identity || rehearsal.Manifest == nil ||
		rehearsal.Manifest.BackupID != metadata.BackupID || rehearsal.BackupDigest != metadata.ArtifactDigest {
		t.Fatalf("preservation identity drifted: metadata=%#v rehearsal=%#v", metadata, rehearsal)
	}
	entries, err := os.ReadDir(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "database.db,metadata.json" {
		t.Fatalf("preservation directory contains non-contract artifacts: %v", names)
	}
}

func TestPreservationRejectsChangedIdentityAndCorruptSnapshot(t *testing.T) {
	t.Run("metadata disagrees with rehearsed manifest", func(t *testing.T) {
		fixture := newPreservationFixture(t)
		metadata := fixture.metadata
		metadata.AppVersion += "-changed"
		metadata.Identity = preservationIdentity(metadata)
		if err := writePreservationMetadata(metadata, func(string) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadSysupgradePreservation(context.Background()); !errors.Is(err, ErrPreservationChanged) {
			t.Fatalf("changed metadata was accepted: %v", err)
		}
	})

	t.Run("snapshot is corrupt", func(t *testing.T) {
		newPreservationFixture(t)
		if err := os.WriteFile(preservationSnapshotPath, []byte("not a SQLite backup"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadSysupgradePreservation(context.Background()); err == nil {
			t.Fatal("corrupt preservation snapshot was accepted")
		}
	})
}

func TestIncompleteDurabilityAndPartialStateFailClosed(t *testing.T) {
	t.Run("metadata directory sync fails", func(t *testing.T) {
		fixture := newPreservationFixture(t)
		if err := os.Remove(preservationMetadataPath); err != nil {
			t.Fatal(err)
		}
		syncFailure := errors.New("directory sync unavailable")
		if err := writePreservationMetadata(fixture.metadata, func(string) error { return syncFailure }); !errors.Is(err, syncFailure) {
			t.Fatalf("durability failure was accepted: %v", err)
		}
	})

	t.Run("snapshot without metadata blocks startup restore", func(t *testing.T) {
		fixture := newPreservationFixture(t)
		closeAndRemoveLiveDatabase(t, fixture.databasePath)
		if err := os.Remove(preservationMetadataPath); err != nil {
			t.Fatal(err)
		}
		manager := datalifecycle.NewManager()
		manager.Admit = func(string) bool { return true }
		environment := testPreservationEnvironment()
		if err := restoreSysupgradePreservationIfNeeded(context.Background(), environment, manager); !errors.Is(err, ErrPreservationAmbiguous) {
			t.Fatalf("partial preservation state was accepted: %v", err)
		}
	})
}

func TestPreservedSnapshotRestoresThroughDataLifecycle(t *testing.T) {
	fixture := newPreservationFixture(t)
	closeAndRemoveLiveDatabase(t, fixture.databasePath)
	_ = os.Remove(filepath.Join(filepath.Dir(fixture.databasePath), "initial-admin.txt"))
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); err != nil {
		t.Fatal(err)
	}
	metadata, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if metadata.State != PreservationStateRestored || !strings.HasPrefix(metadata.RestoreOperationID, "data-operation:") || metadata.RestoreOperationRev == 0 {
		t.Fatalf("restore metadata was not durably completed: %#v", metadata)
	}
	if _, err := os.Lstat(preservationSnapshotPath); !os.IsNotExist(err) {
		t.Fatalf("restored bulk snapshot remains after terminal fence: %v", err)
	}
	if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); err != nil {
		t.Fatalf("reboot-style restored tombstone reconciliation: %v", err)
	}
	if err := dbsqlite.Init(fixture.databasePath); err != nil {
		t.Fatal(err)
	}
	var clients int64
	if err := dbsqlite.DB().Model(&model.Client{}).Where("name = ?", "preserved-wal-client").Count(&clients).Error; err != nil {
		t.Fatal(err)
	}
	if clients != 1 {
		t.Fatalf("restored logical snapshot contains %d preserved clients, want 1", clients)
	}
}

func TestInterruptedStartupRestoreIsRecoveredAndRetried(t *testing.T) {
	fixture := newPreservationFixture(t)
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	metadata := fixture.metadata
	metadata.State = PreservationStateRestoring
	metadata.Identity = preservationIdentity(metadata)
	if err := writePreservationMetadata(metadata, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); err != nil {
		t.Fatal(err)
	}
	restored, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if restored.State != PreservationStateRestored || restored.RestoreOperationRev == 0 {
		t.Fatalf("interrupted restore did not converge: %#v", restored)
	}
}

func TestBackupCompletionWritesTombstoneAndReclaimsBulk(t *testing.T) {
	fixture := newPreservationFixture(t)
	environment := testPreservationEnvironment()
	if err := FinalizeSysupgradePreservationBackup(context.Background(), environment, true); err != nil {
		t.Fatal(err)
	}
	metadata, rehearsal, err := LoadSysupgradePreservation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if metadata.State != PreservationStateBackupDone || metadata.BackupCompletedAt < metadata.PreparedAt || rehearsal.Manifest != nil {
		t.Fatalf("backup terminal tombstone = %#v rehearsal=%#v", metadata, rehearsal)
	}
	if _, err := os.Lstat(preservationSnapshotPath); !os.IsNotExist(err) {
		t.Fatalf("terminal bulk snapshot remains: %v", err)
	}
	if err := FinalizeSysupgradePreservationBackup(context.Background(), environment, true); err != nil {
		t.Fatalf("idempotent terminal cleanup: %v", err)
	}
	if _, err := os.Stat(fixture.databasePath); err != nil {
		t.Fatalf("backup completion changed the live database: %v", err)
	}
}

func TestFailedBackupWritesDistinctTombstoneAndReclaimsBulk(t *testing.T) {
	newPreservationFixture(t)
	if err := FinalizeSysupgradePreservationBackup(context.Background(), testPreservationEnvironment(), false); err != nil {
		t.Fatal(err)
	}
	metadata, rehearsal, err := LoadSysupgradePreservation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if metadata.State != PreservationStateBackupFail || metadata.BackupFailedAt < metadata.PreparedAt ||
		metadata.BackupCompletedAt != 0 || rehearsal.Manifest != nil {
		t.Fatalf("failed-backup terminal tombstone = %#v rehearsal=%#v", metadata, rehearsal)
	}
	if _, err := os.Lstat(preservationSnapshotPath); !os.IsNotExist(err) {
		t.Fatalf("failed-backup bulk snapshot remains: %v", err)
	}
}

func TestBackupCleanupDebtRetriesFromTerminalMetadata(t *testing.T) {
	t.Run("unlink failure", func(t *testing.T) {
		newPreservationFixture(t)
		environment := testPreservationEnvironment()
		removeFailure := errors.New("injected snapshot unlink failure")
		environment.RemoveFile = func(name string) error {
			if name == preservationSnapshotPath {
				return removeFailure
			}
			return os.Remove(name)
		}
		if err := FinalizeSysupgradePreservationBackup(context.Background(), environment, true); !errors.Is(err, removeFailure) {
			t.Fatalf("cleanup failure disappeared: %v", err)
		}
		metadata, _, err := LoadSysupgradePreservation(context.Background())
		if err != nil || metadata.State != PreservationStateBackupDone {
			t.Fatalf("cleanup debt lacks terminal fence: %#v, %v", metadata, err)
		}
		if _, err := os.Stat(preservationSnapshotPath); err != nil {
			t.Fatalf("failed unlink did not retain bulk debt: %v", err)
		}
		if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); err != nil {
			t.Fatalf("startup cleanup retry: %v", err)
		}
		if _, err := os.Lstat(preservationSnapshotPath); !os.IsNotExist(err) {
			t.Fatalf("startup retry retained bulk snapshot: %v", err)
		}
	})

	t.Run("directory sync failure after unlink", func(t *testing.T) {
		newPreservationFixture(t)
		environment := testPreservationEnvironment()
		syncCalls := 0
		syncFailure := errors.New("injected cleanup directory sync failure")
		environment.SyncDirectory = func(string) error {
			syncCalls++
			if syncCalls == 2 {
				return syncFailure
			}
			return nil
		}
		if err := FinalizeSysupgradePreservationBackup(context.Background(), environment, true); !errors.Is(err, syncFailure) {
			t.Fatalf("cleanup sync failure disappeared: %v", err)
		}
		metadata, _, err := LoadSysupgradePreservation(context.Background())
		if err != nil || metadata.State != PreservationStateBackupDone {
			t.Fatalf("post-unlink cleanup debt lacks terminal fence: %#v, %v", metadata, err)
		}
		if _, err := os.Lstat(preservationSnapshotPath); !os.IsNotExist(err) {
			t.Fatalf("bulk snapshot was not unlinked before sync debt: %v", err)
		}
		retrySync := 0
		retry := testPreservationEnvironment()
		retry.SyncDirectory = func(string) error { retrySync++; return nil }
		if err := RestoreSysupgradePreservationIfNeeded(context.Background(), retry); err != nil || retrySync != 1 {
			t.Fatalf("terminal directory-sync retry calls=%d error=%v", retrySync, err)
		}
	})
}

func TestStartupReclaimsInterruptedPartialPreservationFiles(t *testing.T) {
	newPreservationFixture(t)
	for _, name := range []string{preservationSnapshotPath + ".partial", preservationMetadataPath + ".partial"} {
		if err := os.WriteFile(name, []byte("interrupted-generation\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{preservationSnapshotPath + ".partial", preservationMetadataPath + ".partial", preservationSnapshotPath} {
		if _, err := os.Lstat(name); !os.IsNotExist(err) {
			t.Fatalf("startup retained terminal/partial preservation member %s: %v", name, err)
		}
	}
	metadata, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil || metadata.State != PreservationStateBackupFail {
		t.Fatalf("partial cleanup terminal metadata = %#v, %v", metadata, err)
	}
}

func TestStartupReclaimsUnboundFinalSnapshotOnlyWhenLiveDatabaseExists(t *testing.T) {
	t.Run("live database makes unbound snapshot terminal", func(t *testing.T) {
		newPreservationFixture(t)
		if err := os.Remove(preservationMetadataPath); err != nil {
			t.Fatal(err)
		}
		if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(preservationSnapshotPath); !os.IsNotExist(err) {
			t.Fatalf("unbound preparation snapshot remains beside live database: %v", err)
		}
	})

	t.Run("missing live database preserves fail-closed ambiguity", func(t *testing.T) {
		fixture := newPreservationFixture(t)
		closeAndRemoveLiveDatabase(t, fixture.databasePath)
		if err := os.Remove(preservationMetadataPath); err != nil {
			t.Fatal(err)
		}
		if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); !errors.Is(err, ErrPreservationAmbiguous) {
			t.Fatalf("unbound extracted snapshot was reclaimed without a live database: %v", err)
		}
		if _, err := os.Stat(preservationSnapshotPath); err != nil {
			t.Fatalf("fail-closed unbound extracted snapshot was removed: %v", err)
		}
	})
}

func TestSuccessfulRestoreCleanupFailureDoesNotReplayRestore(t *testing.T) {
	fixture := newPreservationFixture(t)
	closeAndRemoveLiveDatabase(t, fixture.databasePath)
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	environment := testPreservationEnvironment()
	syncCalls := 0
	cleanupFailure := errors.New("injected restored snapshot cleanup failure")
	environment.SyncDirectory = func(string) error {
		syncCalls++
		if syncCalls == 3 {
			return cleanupFailure
		}
		return nil
	}
	if err := RestoreSysupgradePreservationIfNeeded(context.Background(), environment); !errors.Is(err, cleanupFailure) {
		t.Fatalf("successful restore cleanup failure disappeared: %v", err)
	}
	metadata, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil || metadata.State != PreservationStateRestored || metadata.RestoreOperationRev == 0 {
		t.Fatalf("restore fence before cleanup debt = %#v, %v", metadata, err)
	}
	operationID, operationRevision := metadata.RestoreOperationID, metadata.RestoreOperationRev
	if _, err := os.Stat(fixture.databasePath); err != nil {
		t.Fatalf("cleanup failure removed the successfully restored database: %v", err)
	}
	if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); err != nil {
		t.Fatalf("restored cleanup retry: %v", err)
	}
	metadata, _, err = LoadSysupgradePreservation(context.Background())
	if err != nil || metadata.RestoreOperationID != operationID || metadata.RestoreOperationRev != operationRevision {
		t.Fatalf("cleanup retry replayed restore: %#v, %v", metadata, err)
	}
}

func TestPreservationAdmissionModelsOnlyOneLiveGeneration(t *testing.T) {
	want := uint64(dbbackup.MaxRestoreBytes) + uint64(MaxPreservationMetadata)
	if got, err := preservationRequiredBytes(dbbackup.MaxRestoreBytes); err != nil || got != want {
		t.Fatalf("maximum live-generation admission=%d want=%d error=%v", got, want, err)
	}
	for _, invalid := range []int64{0, -1, dbbackup.MaxRestoreBytes + 1} {
		if _, err := preservationRequiredBytes(invalid); !errors.Is(err, ErrPreservationAmbiguous) {
			t.Fatalf("invalid modeled snapshot size %d was admitted: %v", invalid, err)
		}
	}
}

func TestTerminalTombstoneWithoutLiveDatabaseDoesNotReplay(t *testing.T) {
	fixture := newPreservationFixture(t)
	if err := FinalizeSysupgradePreservationBackup(context.Background(), testPreservationEnvironment(), true); err != nil {
		t.Fatal(err)
	}
	closeAndRemoveLiveDatabase(t, fixture.databasePath)
	if err := RestoreSysupgradePreservationIfNeeded(context.Background(), testPreservationEnvironment()); !errors.Is(err, ErrPreservationAmbiguous) {
		t.Fatalf("terminal tombstone without live database was replayed or ignored: %v", err)
	}
	metadata, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil || metadata.State != PreservationStateBackupDone {
		t.Fatalf("terminal no-replay fence changed: %#v, %v", metadata, err)
	}
}

func TestNextPreparationSupersedesTerminalTombstoneWithOneLiveGeneration(t *testing.T) {
	fixture := newPreservationFixture(t)
	environment := testPreservationEnvironment()
	if err := FinalizeSysupgradePreservationBackup(context.Background(), environment, true); err != nil {
		t.Fatal(err)
	}
	terminal, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil || terminal.State != PreservationStateBackupDone {
		t.Fatalf("terminal predecessor = %#v, %v", terminal, err)
	}
	if err := reconcileBeforePreservationPreparation(context.Background(), environment); err != nil {
		t.Fatal(err)
	}
	exportPath, cleanup, err := dbbackup.PrepareExportContext(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	export, err := os.Open(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, rehearsalErr := dbbackup.Rehearse(context.Background(), export)
	closeErr := export.Close()
	if rehearsalErr != nil || closeErr != nil || !rehearsal.Possible || rehearsal.Manifest == nil {
		t.Fatalf("next-generation rehearsal=%#v errors=%v", rehearsal, errors.Join(rehearsalErr, closeErr))
	}
	if required, err := preservationRequiredBytes(rehearsal.BackupBytes); err != nil ||
		required != uint64(rehearsal.BackupBytes)+uint64(MaxPreservationMetadata) {
		t.Fatalf("next-generation required bytes=%d error=%v", required, err)
	}
	if err := installPreservationSnapshot(context.Background(), exportPath, rehearsal, environment.SyncDirectory); err != nil {
		t.Fatal(err)
	}
	manifest := rehearsal.Manifest
	next := PreservationMetadataV1{
		Schema: PreservationSchemaV1, State: PreservationStatePrepared, ArtifactPath: preservationSnapshotPath,
		ArtifactDigest: rehearsal.BackupDigest, ArtifactBytes: rehearsal.BackupBytes, BackupID: manifest.BackupID,
		RehearsalRevision: rehearsal.Revision, AppVersion: manifest.AppVersion, CoreSchema: manifest.CoreSchema,
		DeploymentProfile: manifest.DeploymentProfile, DeploymentRevision: manifest.DeploymentRevision,
		PreparedAt: terminal.BackupCompletedAt + 1,
	}
	next.Identity = preservationIdentity(next)
	if err := writePreservationMetadata(next, environment.SyncDirectory); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadSysupgradePreservation(context.Background())
	if err != nil || loaded.State != PreservationStatePrepared || loaded.Identity == terminal.Identity {
		t.Fatalf("next live generation = %#v predecessor=%#v error=%v", loaded, terminal, err)
	}
	entries, err := os.ReadDir(fixture.root)
	if err != nil || len(entries) != 2 {
		t.Fatalf("next preparation retained more than one live generation: entries=%v error=%v", entries, err)
	}
}

func newPreservationFixture(t *testing.T) preservationFixture {
	t.Helper()
	_ = dbsqlite.Close()
	databaseFolder, err := os.MkdirTemp("", "solovey-openwrt-preservation-*")
	if err != nil {
		t.Fatal(err)
	}
	oldRoot, oldSnapshot, oldMetadata := preservationRootPath, preservationSnapshotPath, preservationMetadataPath
	preservationRootPath = filepath.Join(databaseFolder, "sysupgrade-preservation")
	preservationSnapshotPath = filepath.Join(preservationRootPath, "database.db")
	preservationMetadataPath = filepath.Join(preservationRootPath, "metadata.json")
	t.Setenv("SUI_DB_FOLDER", databaseFolder)
	t.Cleanup(func() {
		_ = dbsqlite.Close()
		preservationRootPath, preservationSnapshotPath, preservationMetadataPath = oldRoot, oldSnapshot, oldMetadata
		time.Sleep(25 * time.Millisecond)
		_ = os.RemoveAll(databaseFolder)
	})
	if err := os.MkdirAll(preservationRootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	databasePath := configstorage.GetDBPath()
	if err := dbsqlite.Init(databasePath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Client{Enable: true, Name: "preserved-wal-client", SubSecret: "preserved-wal-secret", Inbounds: []byte("[]"), Links: []byte("[]")}).Error; err != nil {
		t.Fatal(err)
	}
	exportPath, cleanup, err := dbbackup.PrepareExportContext(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	export, err := os.Open(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, rehearsalErr := dbbackup.Rehearse(context.Background(), export)
	closeErr := export.Close()
	if rehearsalErr != nil || closeErr != nil || !rehearsal.Possible || rehearsal.Manifest == nil {
		t.Fatalf("fixture rehearsal failed: rehearsal=%#v errors=%v", rehearsal, errors.Join(rehearsalErr, closeErr))
	}
	if err := installPreservationSnapshot(context.Background(), exportPath, rehearsal, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	manifest := rehearsal.Manifest
	metadata := PreservationMetadataV1{
		Schema: PreservationSchemaV1, State: PreservationStatePrepared, ArtifactPath: preservationSnapshotPath,
		ArtifactDigest: rehearsal.BackupDigest, ArtifactBytes: rehearsal.BackupBytes, BackupID: manifest.BackupID,
		RehearsalRevision: rehearsal.Revision, AppVersion: manifest.AppVersion, CoreSchema: manifest.CoreSchema,
		DeploymentProfile: manifest.DeploymentProfile, DeploymentRevision: manifest.DeploymentRevision,
		PreparedAt: time.Unix(1_900_000_000, 0).UTC().Unix(),
	}
	metadata.Identity = preservationIdentity(metadata)
	if err := writePreservationMetadata(metadata, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return preservationFixture{databasePath: databasePath, root: preservationRootPath, metadata: metadata}
}

func closeAndRemoveLiveDatabase(t *testing.T, databasePath string) {
	t.Helper()
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{databasePath, databasePath + "-wal", databasePath + "-shm", databasePath + "-journal"} {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
}

func testPreservationEnvironment() PreservationEnvironment {
	return PreservationEnvironment{
		InspectDurableState: func(string, uint64) (DurableStateEvidence, error) {
			return DurableStateEvidence{MountPath: "/", Persistent: true, AvailableBytes: 1 << 30, RequiredBytes: 1}, nil
		},
		SyncDirectory: func(string) error { return nil },
		RemoveFile:    os.Remove,
		Now:           func() time.Time { return time.Unix(1_900_000_100, 0) },
	}
}
