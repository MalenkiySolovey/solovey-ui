package importxuicmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/importxui"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestImportXuiCommandDryRunReport(t *testing.T) {
	closeCmdTestDB(t)
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	t.Cleanup(func() { closeCmdTestDB(t) })
	copyCmdFixture(t, "s-ui.db", configstorage.GetDBPath())
	src := copyCmdFixture(t, "x-ui.db", filepath.Join(dir, "x-ui.db"))
	reportPath := filepath.Join(dir, "report.json")
	var out bytes.Buffer

	code := Run([]string{"--src", src, "--dry-run", "--report", reportPath}, &out)
	if code != 0 && strings.Contains(out.String(), "go-sqlite3 requires cgo") {
		t.Skip(out.String())
	}
	if code != 0 {
		t.Fatalf("Run returned %d, output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "Import summary") {
		t.Fatalf("summary was not printed:\n%s", out.String())
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report importxui.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Inbounds.Imported == 0 || report.Summary.Clients.UniqueEmails == 0 {
		t.Fatalf("unexpected report summary: %#v", report.Summary)
	}
}

func copyCmdFixture(t *testing.T, name string, dst string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(wd, "..", "..", "..", "test-db", name)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("test-db fixture %q not available: %v", name, err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

func closeCmdTestDB(t *testing.T) {
	t.Helper()
	if db := dbsqlite.DB(); db != nil {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}
