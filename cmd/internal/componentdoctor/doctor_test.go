package componentdoctor

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func legacyDoctorDatabase(t *testing.T, host string, port int) (*gorm.DB, string) {
	t.Helper()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	path := configstorage.GetDBPath()
	db, err := gorm.Open(gormsqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	for _, statement := range []string{"CREATE TABLE settings (id INTEGER PRIMARY KEY, key TEXT, value TEXT)", "INSERT INTO settings(key,value) VALUES('coreSchemaVersion','1.8')"} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"webListen": host, "webPort": strconv.Itoa(port)} {
		if err := db.Exec("INSERT INTO settings(key,value) VALUES(?,?)", key, value).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, path
}

func TestPanelDoctorReadsConfiguredBindWithoutMigrating(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	writer, path := legacyDoctorDatabase(t, "127.0.0.1", port)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if Run([]string{"--panel"}, &output) != 0 {
		t.Fatal(output.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("health check changed legacy database", err)
	}
	if writer.Migrator().HasTable("clients") || writer.Migrator().HasTable("users") {
		t.Fatal("health check initialized runtime tables")
	}
	// A listener on another address at the same port must not prove this bind.
	if err := writer.Exec("UPDATE settings SET value = '127.0.0.2' WHERE key = 'webListen'").Error; err != nil {
		t.Fatal(err)
	}
	reader, err := dbsqlite.OpenReadOnly(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := reader.DB()
	defer pool.Close()
	if err := checkPanel(context.Background(), reader); err == nil {
		t.Fatal("other local bind was reported healthy")
	}
}

func TestPanelDoctorReadsCommittedSettingsDuringWALWriter(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	writer, path := legacyDoctorDatabase(t, "127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	if err := writer.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		t.Fatal(err)
	}
	tx := writer.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("UPDATE settings SET value = 'uncommitted' WHERE key = 'coreSchemaVersion'").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	reader, err := dbsqlite.OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal("read-only check waited on a writer", err)
	}
	pool, _ := reader.DB()
	defer pool.Close()
	if err := checkPanel(ctx, reader); err != nil {
		t.Fatal("health check cannot read committed state under WAL", err)
	}
}

func TestPanelDoctorIPv6ConfiguredListener(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback unavailable")
	}
	defer listener.Close()
	_, _ = legacyDoctorDatabase(t, "::1", listener.Addr().(*net.TCPAddr).Port)
	var output bytes.Buffer
	if Run([]string{"--panel"}, &output) != 0 {
		t.Fatal(output.String())
	}
}

func TestDoctorMissingDatabaseFailsWithoutCreatingState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	t.Setenv("SUI_DB_FOLDER", dir)
	var output bytes.Buffer
	if Run([]string{"--components"}, &output) != 1 {
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("doctor created missing state", err)
	}
}

func TestPanelProbeHostsPreservesConfiguredSemantics(t *testing.T) {
	for _, test := range []struct {
		bind  string
		hosts []string
	}{
		{"", []string{"127.0.0.1", "::1"}}, {"0.0.0.0", []string{"127.0.0.1"}},
		{"::", []string{"::1"}}, {"0:0:0:0:0:0:0:0", []string{"::1"}},
		{"127.0.0.2", []string{"127.0.0.2"}}, {"::1", []string{"::1"}}, {"localhost", []string{"localhost"}},
	} {
		if got := probeHosts(test.bind); !reflect.DeepEqual(got, test.hosts) {
			t.Fatalf("bind %q: %v", test.bind, got)
		}
	}
}
