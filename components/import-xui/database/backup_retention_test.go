//go:build !minimal

package importxui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestPrunePreImportBackups_KeepsNewest guards the runaway-backup fix: a slow
// import that the client resubmits used to leave dozens of
// s-ui-pre-xui-import-*.db files in the db directory. Pruning must keep only
// the newest N and never touch unrelated files.
func TestPrunePreImportBackups_KeepsNewest(t *testing.T) {
	dir := makeImportXUITempDir(t)
	const total = 15
	for i := 1; i <= total; i++ {
		name := filepath.Join(dir, fmt.Sprintf("s-ui-pre-xui-import-%010d.db", 1700000000+i))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Unrelated files must survive pruning.
	other := filepath.Join(dir, "s-ui.db")
	if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	prunePreImportBackups(dir, preImportBackupRetention)

	matches, err := filepath.Glob(filepath.Join(dir, "s-ui-pre-xui-import-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != preImportBackupRetention {
		t.Fatalf("kept %d backups, want %d", len(matches), preImportBackupRetention)
	}
	newest := filepath.Join(dir, fmt.Sprintf("s-ui-pre-xui-import-%010d.db", 1700000000+total))
	if _, err := os.Stat(newest); err != nil {
		t.Fatalf("newest backup was pruned: %v", err)
	}
	oldest := filepath.Join(dir, fmt.Sprintf("s-ui-pre-xui-import-%010d.db", 1700000001))
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatalf("oldest backup should have been pruned, stat err=%v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}

// TestPrunePreImportBackups_NoopUnderLimit verifies nothing is deleted when the
// count is at or below the retention limit.
func TestPrunePreImportBackups_NoopUnderLimit(t *testing.T) {
	dir := makeImportXUITempDir(t)
	for i := 1; i <= 3; i++ {
		name := filepath.Join(dir, fmt.Sprintf("s-ui-pre-xui-import-%010d.db", 1700000000+i))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prunePreImportBackups(dir, preImportBackupRetention)
	matches, _ := filepath.Glob(filepath.Join(dir, "s-ui-pre-xui-import-*.db"))
	if len(matches) != 3 {
		t.Fatalf("kept %d backups, want 3 (no pruning under limit)", len(matches))
	}
}

func TestPublishPreImportBackupDoesNotReplaceExistingBackup(t *testing.T) {
	dir := makeImportXUITempDir(t)
	staged := filepath.Join(dir, "staged.db")
	if err := os.WriteFile(staged, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "s-ui-pre-xui-import-1700000000.db")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	published, err := publishPreImportBackup(staged, dir, 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(published) != "s-ui-pre-xui-import-1700000000-01.db" {
		t.Fatalf("published path=%q", published)
	}
	old, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(old) != "old" {
		t.Fatalf("existing backup was replaced: %q", old)
	}
	fresh, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	if string(fresh) != "new" {
		t.Fatalf("published backup=%q", fresh)
	}
}
