package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMasterLifecycleCandidateRollbackAndIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "master.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	abort := errors.New("candidate rejected")
	if err := db.Transaction(func(tx *gorm.DB) error {
		master, err := EnsureMasterSecret(tx)
		if err != nil || len(master) != 32 {
			t.Fatal("candidate master creation failed")
		}
		return abort
	}); !errors.Is(err, abort) {
		t.Fatal("candidate did not abort")
	}
	if _, err := Find(db, settingcatalog.SecretKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("failed candidate published a master")
	}
	first, err := EnsureMasterSecret(db)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureMasterSecret(db)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("existing master was replaced")
	}
	if err := UpsertValue(db, settingcatalog.SecretKey, ""); err != nil {
		t.Fatal(err)
	}
	empty, err := EnsureMasterSecret(db)
	if err != nil || len(empty) != 0 {
		t.Fatal("an existing empty setting was implicitly rotated")
	}
	if _, err := EnsureMasterSecret(nil); err == nil {
		t.Fatal("missing database accepted")
	}
}
