//go:build !minimal

package source

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

type failedSourceHandle struct {
	gorm.ConnPool
	err error
}

func (f failedSourceHandle) GetDBConn() (*sql.DB, error) { return nil, f.err }

func sourceReaders() map[string]func(*Database) error {
	return map[string]func(*Database) error{
		"inbounds":         func(s *Database) error { return s.EachInbound(func(InboundRow) error { return nil }) },
		"client iteration": func(s *Database) error { return s.EachClientTraffic(func(ClientTraffic) error { return nil }) },
		"clients":          func(s *Database) error { _, err := s.Clients(); return err },
		"inbound count":    func(s *Database) error { _, err := s.InboundCount(); return err },
		"settings":         func(s *Database) error { _, err := s.Settings(); return err },
		"users":            func(s *Database) error { _, err := s.Users(); return err },
		"history":          func(s *Database) error { _, err := s.OutboundTraffics(); return err },
		"routing":          func(s *Database) error { _, err := s.XrayConfig(); return err },
	}
}

func TestSourceReadersPreserveHandleFailureWithoutPanic(t *testing.T) {
	cause := errors.New("injected source extraction failure")
	for name, src := range map[string]*Database{
		"nil": nil, "zero": {}, "malformed": {db: &gorm.DB{}},
		"extraction": {db: &gorm.DB{Config: &gorm.Config{ConnPool: failedSourceHandle{err: cause}}}, dialect: Dialect3XUIMHSanaei{}},
	} {
		t.Run(name, func(t *testing.T) {
			for reader, read := range sourceReaders() {
				err := read(src)
				if err == nil || name == "extraction" && !errors.Is(err, cause) {
					t.Fatalf("%s did not preserve source failure: %v", reader, err)
				}
			}
			if err := src.validate(); err == nil || name == "extraction" && !errors.Is(err, cause) {
				t.Fatalf("initial validation lost source failure: %v", err)
			}
			src.Close()
		})
	}
}

func TestSourceReadersRejectClosedHandleAndUninitializedDialect(t *testing.T) {
	path := sourceFixture(t)
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(src.Close)
	dialect := src.dialect
	src.dialect = nil
	for name, read := range sourceReaders() {
		if err := read(src); !errors.Is(err, ErrDialectUnknown) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	src.dialect = dialect
	src.Close()
	for name, read := range sourceReaders() {
		if err := read(src); err == nil {
			t.Fatalf("closed source succeeded: %s", name)
		}
	}
	if _, err := dialect.Detect(nil); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatal(err)
	}
	for name, read := range sourceReaders() {
		if err := read(&Database{db: &gorm.DB{Config: &gorm.Config{}}, dialect: dialect}); err == nil {
			t.Fatalf("missing SQL pool succeeded: %s", name)
		}
	}
}

func TestSourceRestrictsEveryConnectionAndLeavesInputUnchanged(t *testing.T) {
	path := sourceFixture(t)
	before, err := Hash(path)
	if err != nil {
		t.Fatal(err)
	}
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	db, err := src.sqlDB()
	if err != nil {
		t.Fatal(err)
	}
	var conns []*sql.Conn
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	for i := 0; i < 3; i++ {
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		for pragma, want := range map[string]int{"query_only": 1, "trusted_schema": 0} {
			var got int
			if err := conn.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
				t.Fatalf("connection %d %s=%d err=%v", i, pragma, got, err)
			}
		}
		if _, err := conn.ExecContext(t.Context(), "CREATE TABLE forbidden (id INTEGER)"); err == nil {
			t.Fatal("source connection accepted a write")
		}
	}
	after, err := Hash(path)
	if err != nil || before != after {
		t.Fatal("read-only source was changed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (sourceConnector{dsn: path}).Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSourceOptionalTablesAndFailureReleaseRows(t *testing.T) {
	path := sourceFixture(t)
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	for name, read := range sourceReaders() {
		if err := read(src); err != nil {
			t.Fatalf("compatible absent optional table %s: %v", name, err)
		}
	}
	db, err := src.sqlDB()
	if err != nil {
		t.Fatal(err)
	}
	if db.Stats().InUse != 0 {
		t.Fatal("readers retained live rows")
	}
	src.Close()
	writer, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("INSERT INTO client_traffics (id, up) VALUES (1, 'invalid-number')"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	src, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := src.Clients(); err == nil {
		t.Fatal("invalid source scan succeeded")
	}
	db, err = src.sqlDB()
	if err != nil || db.Stats().InUse != 0 {
		t.Fatal("failed reader retained live rows")
	}
}

func TestSourceOpenFailureReleasesPartialPool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.db")
	if err := os.WriteFile(path, []byte("not a SQLite database"), 0600); err != nil {
		t.Fatal(err)
	}
	if src, err := Open(path); err == nil || src != nil {
		t.Fatal("invalid source was accepted")
	}
	if err := os.Rename(path, path+".closed"); err != nil {
		t.Fatal("failed open retained source file", err)
	}
}

func sourceFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{"CREATE TABLE inbounds (id INTEGER)", "CREATE TABLE client_traffics (id INTEGER, up INTEGER)"} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
