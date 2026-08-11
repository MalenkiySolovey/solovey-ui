package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestBundledSQLiteRuntimeAndWALResetSafetyGate(t *testing.T) {
	db := openWALRegressionDB(t, filepath.Join(t.TempDir(), "runtime.db"))
	defer db.Close()
	var version, sourceID, journal string
	if err := db.QueryRow("SELECT sqlite_version(), sqlite_source_id()").Scan(&version, &sourceID); err != nil {
		t.Fatal(err)
	}
	if version != SQLiteRuntimeVersion || sourceID != SQLiteRuntimeSourceID {
		t.Fatalf("bundled SQLite identity version=%q source=%q", version, sourceID)
	}
	if SQLiteModuleVersion != "v1.14.49" || SQLiteModuleCommit != "cc41b8c87686036ea632cede537ffccef69b370a" ||
		SQLiteModuleSum != "h1:B8jBHC3xhxZgxztrgruTuLucebnULQnx4W7cF7SAE9w=" {
		t.Fatal("bundled go-sqlite3 module identity changed without an explicit runtime posture update")
	}
	if ParseSQLiteVersion(version) < 3_051_003 || !walResetSafe(ParseSQLiteVersion(version)) {
		t.Fatalf("unsafe bundled SQLite runtime %s", version)
	}
	if !strings.Contains(sourceID, "bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc") {
		t.Fatalf("unexpected SQLite source id %q", sourceID)
	}
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || strings.ToLower(journal) != "wal" {
		t.Fatalf("journal=%q err=%v", journal, err)
	}
}

func TestInspectRuntimeReportsPinnedIdentityAndCompilePosture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.db")
	db, err := gorm.Open(gormsqlite.Open(path+"?_journal_mode=WAL&_busy_timeout=3000"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	status, err := InspectRuntime(db)
	if err != nil {
		t.Fatal(err)
	}
	if !status.RuntimePinned || !status.WALResetSafe || !status.WALCapable || status.JournalMode != "wal" ||
		status.RuntimeVersion != SQLiteRuntimeVersion || status.SourceID != SQLiteRuntimeSourceID ||
		len(status.CompileOptions) == 0 || status.Revision == "" {
		t.Fatalf("unexpected SQLite runtime posture: %#v", status)
	}
}

func TestWALConcurrentReadersWritersCheckpointResetAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	db := openWALRegressionDB(t, path)
	if _, err := db.Exec("CREATE TABLE events(id INTEGER PRIMARY KEY, writer INTEGER NOT NULL, value TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	const writers, rowsPerWriter = 4, 120
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var group sync.WaitGroup
	errorsChannel := make(chan error, writers+2)
	for writer := 0; writer < writers; writer++ {
		writer := writer
		group.Add(1)
		go func() {
			defer group.Done()
			for row := 0; row < rowsPerWriter; row++ {
				if _, err := db.ExecContext(ctx, "INSERT INTO events(writer,value) VALUES(?,?)", writer, fmt.Sprintf("%d:%d", writer, row)); err != nil {
					errorsChannel <- err
					return
				}
			}
		}()
	}
	for reader := 0; reader < 2; reader++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for iteration := 0; iteration < 80; iteration++ {
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&count); err != nil {
					errorsChannel <- err
					return
				}
			}
		}()
	}
	group.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 24; cycle++ {
		if _, err := db.ExecContext(ctx, "INSERT INTO events(writer,value) VALUES(-1,?)", fmt.Sprintf("checkpoint:%d", cycle)); err != nil {
			t.Fatal(err)
		}
		mode := "PASSIVE"
		if cycle%3 == 0 {
			mode = "TRUNCATE"
		}
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint("+mode+")"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openWALRegressionDB(t, path)
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM events").Scan(&count); err != nil || count != writers*rowsPerWriter+24 {
		t.Fatalf("reopened count=%d err=%v", count, err)
	}
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
}

func TestSQLiteBusyTimeoutWaitsForWriterAndSucceeds(t *testing.T) {
	db := openWALRegressionDB(t, filepath.Join(t.TempDir(), "busy.db"))
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE busy_rows(id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := first.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	done := make(chan error, 1)
	go func() {
		_, insertErr := second.ExecContext(ctx, "INSERT INTO busy_rows(value) VALUES('waited')")
		done <- insertErr
	}()
	time.Sleep(150 * time.Millisecond)
	if _, err := first.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond || elapsed > 4*time.Second {
		t.Fatalf("busy timeout wait=%v", elapsed)
	}
}

func TestSQLiteProcessRestartDurability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	for _, mode := range []string{"write", "read"} {
		command := exec.Command(os.Args[0], "-test.run=TestSQLiteProcessRestartHelper", "-test.v") // #nosec G204 -- current test binary only.
		command.Env = append(os.Environ(), "SUI_SQLITE_RESTART_HELPER="+mode, "SUI_SQLITE_RESTART_PATH="+path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("restart helper %s failed: %v\n%s", mode, err, output)
		}
	}
}

func TestCloseForFileSwapCheckpointsAndDetachesExactLiveDatabase(t *testing.T) {
	_ = Close()
	path := filepath.Join(t.TempDir(), "file-swap.db")
	if err := open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	if err := DB().Exec("CREATE TABLE swap_rows(id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO swap_rows(value) VALUES('durable')").Error; err != nil {
		t.Fatal(err)
	}
	if err := CloseForFileSwap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if DB() != nil || currentDatabasePath() != "" {
		t.Fatal("file-swap close left the live database attached")
	}
	observed := openWALRegressionDB(t, path)
	defer observed.Close()
	var value string
	if err := observed.QueryRow("SELECT value FROM swap_rows WHERE id = 1").Scan(&value); err != nil || value != "durable" {
		t.Fatalf("checkpointed value=%q err=%v", value, err)
	}
}

func TestSQLiteProcessRestartHelper(t *testing.T) {
	mode, path := os.Getenv("SUI_SQLITE_RESTART_HELPER"), os.Getenv("SUI_SQLITE_RESTART_PATH")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	db := openWALRegressionDB(t, path)
	defer db.Close()
	switch mode {
	case "write":
		if _, err := db.Exec("CREATE TABLE restart_rows(id INTEGER PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < 50; index++ {
			if _, err := db.Exec("INSERT INTO restart_rows(value) VALUES(?)", strconv.Itoa(index)); err != nil {
				t.Fatal(err)
			}
		}
	case "read":
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM restart_rows").Scan(&count); err != nil || count != 50 {
			t.Fatalf("restart count=%d err=%v", count, err)
		}
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

func openWALRegressionDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=3000&_synchronous=NORMAL&_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return db
}

func BenchmarkSQLiteRuntimeInspection(b *testing.B) {
	path := filepath.Join(b.TempDir(), "runtime-benchmark.db")
	db, err := gorm.Open(gormsqlite.Open(path+"?_journal_mode=WAL&_busy_timeout=3000"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		b.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = sqlDB.Close() })
	b.ReportAllocs()
	for b.Loop() {
		status, inspectErr := InspectRuntime(db)
		if inspectErr != nil || !status.RuntimePinned || !status.WALCapable {
			b.Fatal(inspectErr)
		}
	}
}
