//go:build !minimal

package datalifecycle

import (
	"context"
	"errors"
	componentmanifest "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultDropDataPublishesPortableConsumableRecoveryBackup(t *testing.T) {
	databaseRoot := filepath.Join(t.TempDir(), "long-"+strings.Repeat("a", 72), "nested-"+strings.Repeat("b", 48))
	t.Setenv("SUI_DB_FOLDER", databaseRoot)
	if err := os.MkdirAll(databaseRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	owner := registerDropDataFixtureOwner()
	installedPath := filepath.Join(databaseRoot, "installed-components.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{
		ID: owner.ID, Delivery: componentmanifest.DeliveryInProcess, Installed: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	for _, table := range owner.Database.Tables {
		if err := dbsqlite.DB().Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, value TEXT)").Error; err != nil {
			t.Fatal(err)
		}
		if err := dbsqlite.DB().Exec("INSERT INTO "+table+"(value) VALUES (?)", "portable-default-backup").Error; err != nil {
			t.Fatal(err)
		}
	}
	manager := NewManager()
	manager.DB = dbsqlite.DB
	manager.Root = filepath.Join(databaseRoot, "recovery", "drop-data")
	manager.RestoreRoot = filepath.Join(databaseRoot, "recovery", "restore")
	manager.Enabled = func(componentmanifest.Manifest) (bool, error) { return false, nil }
	manager.Admit = func(string) bool { return true }
	manager.Drop = func(ctx context.Context, ownerID string) error {
		if ownerID != owner.ID {
			return errors.New("unexpected owner")
		}
		for _, table := range owner.Database.Tables {
			if err := dbsqlite.DB().WithContext(ctx).Migrator().DropTable(table); err != nil {
				return err
			}
		}
		return nil
	}
	preview, err := manager.Preview(context.Background(), owner.ID)
	if err != nil || len(preview.Blockers) != 0 {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	request := ExecuteRequest{OwnerID: owner.ID, ExpectedPreviewRevision: preview.Revision, IdempotencyKey: "drop-portable-default-recovery",
		Confirmation: "DROP_DATA_DROP_OWNER_FIXTURE", BackupAcknowledged: true}
	operation, err := manager.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != "APPLIED" || !strings.Contains(operation.OperationID, ":") || !validDigest(operation.BackupRef) {
		t.Fatalf("operation=%#v", operation)
	}
	entries, err := os.ReadDir(manager.Root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("recovery entries=%v err=%v", entries, err)
	}
	name := entries[0].Name()
	if name != portableDropRecoveryFilename(operation.OperationID) || strings.ContainsAny(name, `:<>"/\\|?*`) {
		t.Fatalf("portable recovery filename=%q operation=%q", name, operation.OperationID)
	}
	file, artifact, err := manager.OpenRecoveryBackup(context.Background(), operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, err := dbbackup.Rehearse(context.Background(), file)
	if err != nil || rehearsal.Integrity != "VERIFIED" || rehearsal.ManifestStatus != "VERIFIED" ||
		rehearsal.BackupDigest != operation.BackupRef || artifact.BackupRef != operation.BackupRef || artifact.Bytes <= 0 {
		t.Fatalf("artifact=%#v rehearsal=%#v err=%v", artifact, rehearsal, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	replayed, err := manager.Execute(context.Background(), request)
	if err != nil || replayed.OperationID != operation.OperationID || replayed.Revision != operation.Revision {
		t.Fatalf("duplicate replay=%#v err=%v", replayed, err)
	}
	entries, err = os.ReadDir(manager.Root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("duplicate publication entries=%v err=%v", entries, err)
	}
}
