package integrity

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureNoTLSForeignKeyParentRepairsNoTLSInbound(t *testing.T) {
	db := openForeignKeyTestDB(t)
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
CREATE TABLE tls(
	id integer PRIMARY KEY AUTOINCREMENT,
	name text,
	server blob,
	client blob
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
CREATE TABLE inbounds(
	id integer PRIMARY KEY AUTOINCREMENT,
	tag text,
	tls_id integer,
	FOREIGN KEY(tls_id) REFERENCES tls(id)
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO inbounds(tag, tls_id) VALUES(?, ?)", "no-tls", 0).Error; err != nil {
		t.Fatal(err)
	}

	before, err := tlsForeignKeyViolations(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].Table != "inbounds" {
		t.Fatalf("expected one no-TLS inbound violation before repair, got %#v", before)
	}
	if err := EnsureNoTLSForeignKeyParent(db); err != nil {
		t.Fatal(err)
	}
	after, err := tlsForeignKeyViolations(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("foreign-key violations remained after no-TLS parent repair: %#v", after)
	}
}

func openForeignKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared&_foreign_keys=off"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func tlsForeignKeyViolations(db *gorm.DB) ([]struct {
	Table  string
	RowID  int64
	Parent string
	FKID   int
}, error) {
	var rows []struct {
		Table  string
		RowID  int64
		Parent string
		FKID   int
	}
	err := db.Raw("PRAGMA foreign_key_check").Scan(&rows).Error
	return rows, err
}
