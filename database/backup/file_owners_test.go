package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLogicalFilesActualExportRehearsalRestoreAndRollback(t *testing.T) {
	ctx := context.Background()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv(LogicalFileBackupEnv, LogicalFileBackupRequired)
	fileOwners.Lock()
	previous := fileOwners.items
	fileOwners.items = map[string]FileOwner{}
	fileOwners.Unlock()
	t.Cleanup(func() { fileOwners.Lock(); fileOwners.items = previous; fileOwners.Unlock() })
	root := filepath.Join(t.TempDir(), "restored")
	publicationCalls := 0
	failPublication := false
	RegisterFileOwner("test-owner", FileOwner{
		Export: func(context.Context, *gorm.DB) ([]OwnerFile, error) {
			return []OwnerFile{{Key: "fixture", Data: []byte("portable file")}}, nil
		},
		Restore: func(_ context.Context, _ *gorm.DB, files []OwnerFile, publish bool) error {
			if len(files) != 1 || files[0].Key != "fixture" {
				return errors.New("missing fixture")
			}
			if publish {
				publicationCalls++
			}
			_, err := PublishOwnedFile(root, files[0].Data, publish)
			if err == nil && publish && failPublication {
				return errors.New("injected owner publication rejection")
			}
			return err
		},
	})
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if err := dbsqlite.DB().Create(&model.Setting{Key: "logical-fixture", Value: "snapshot"}).Error; err != nil {
		t.Fatal(err)
	}
	data, err := Export("")
	if err != nil {
		t.Fatal(err)
	}
	var encrypted, decrypted bytes.Buffer
	if _, _, err := backupenvelope.SealStream(&encrypted, bytes.NewReader(data), []byte("qualification-fixture-passphrase")); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted.Bytes(), []byte("portable file")) {
		t.Fatal("file plaintext escaped encrypted backup")
	}
	if _, _, err := backupenvelope.OpenStream(&decrypted, bytes.NewReader(encrypted.Bytes()), []byte("qualification-fixture-passphrase"), MaxRestoreBytes); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, decrypted.Bytes()) {
		t.Fatal("encrypted logical backup changed bytes")
	}
	data = decrypted.Bytes()
	r, err := Rehearse(ctx, bytes.NewReader(data))
	if err != nil || !r.Possible || r.Manifest.Files == nil || publicationCalls != 0 {
		t.Fatalf("rehearsal: %v %#v", err, r.ReasonCodes)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("rehearsal wrote files")
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "logical-fixture").Update("value", "live").Error; err != nil {
		t.Fatal(err)
	}
	failPublication = true
	if _, err := RestoreContextDetailed(ctx, bytes.NewReader(data)); err == nil || !strings.Contains(err.Error(), "injected owner publication rejection") {
		t.Fatalf("expected owner rollback: %v", err)
	}
	var setting model.Setting
	if err := dbsqlite.DB().Where("key = ?", "logical-fixture").First(&setting).Error; err != nil || setting.Value != "live" {
		t.Fatal("failed file restore did not preserve exact live DB")
	}
	failPublication = false
	if _, err := RestoreContextDetailed(ctx, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Where("key = ?", "logical-fixture").First(&setting).Error; err != nil || setting.Value != "snapshot" {
		t.Fatal("logical DB state was not restored")
	}
	if publicationCalls != 2 {
		t.Fatalf("unexpected publications %d", publicationCalls)
	}
	if dbsqlite.DB().Migrator().HasTable(BackupFileTable) {
		t.Fatal("restored payload retained a duplicate file authority")
	}
	if err := AbortPendingRestore(); err != nil {
		t.Fatal(err)
	}
}

func TestLogicalFileManifestRejectsCorruption(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "files.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&OwnerFile{}); err != nil {
		t.Fatal(err)
	}
	data := []byte("durable file fixture")
	sum := sha256.Sum256(data)
	f := OwnerFile{Owner: "test-owner", Key: "asset:1", Data: data, Digest: hex.EncodeToString(sum[:])}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	d, err := digestBackupTable(context.Background(), db, "core", BackupFileTable)
	if err != nil {
		t.Fatal(err)
	}
	m := &FileBackupManifest{Schema: "solovey.logical-owner-files/v1", Owners: []string{"test-owner"}, Rows: d.Rows, SchemaDigest: d.SchemaDigest, ContentDigest: d.ContentDigest}
	if err := verifyOwnerFiles(context.Background(), db, m); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&OwnerFile{}).Where("owner = ?", f.Owner).Update("data", []byte("corrupt")).Error; err != nil {
		t.Fatal(err)
	}
	if err := verifyOwnerFiles(context.Background(), db, m); err == nil {
		t.Fatal("tampered payload accepted")
	}
}

func TestImmutableOwnerPublicationPreservesRollback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner", "restored")
	name, err := PublishOwnedFile(root, []byte("new"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("rehearsal published files")
	}
	actual, err := PublishOwnedFile(root, []byte("new"), true)
	if err != nil || name != actual {
		t.Fatal(err)
	}
	if _, err := PublishOwnedFile(root, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("conflict"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishOwnedFile(root, []byte("new"), true); err == nil {
		t.Fatal("conflicting immutable file overwritten")
	}
	data, _ := os.ReadFile(name)
	if string(data) != "conflict" {
		t.Fatal("pre-existing data changed")
	}
}

func TestLogicalFileCapabilityFailsClosed(t *testing.T) {
	t.Setenv(LogicalFileBackupEnv, LogicalFileBackupRequired)
	if err := restoreOwnerFiles(context.Background(), nil, nil, false); err == nil {
		t.Fatal("incomplete backup accepted")
	}
	t.Setenv(LogicalFileBackupEnv, "invalid")
	if _, err := logicalFileBackupRequired(); err == nil {
		t.Fatal("unknown capability accepted")
	}
	t.Setenv(LogicalFileBackupEnv, "")
	if err := restoreOwnerFiles(context.Background(), nil, nil, false); err != nil {
		t.Fatalf("stock contract changed: %v", err)
	}
}
