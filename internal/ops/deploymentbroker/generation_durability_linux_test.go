//go:build linux

package deploymentbroker

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestGenerationUnitSymlinkAndMarkerRemovalFaultMatrix(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	directory := t.TempDir()
	oldTarget := filepath.Join(directory, "old.service")
	newTarget := filepath.Join(directory, "new.service")
	for _, path := range []string{oldTarget, newTarget} {
		if err := os.WriteFile(path, []byte("[Service]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fault := errors.New("generation unit fault")
	for _, test := range []struct {
		name, faultAt string
		wantTarget    string
	}{
		{name: "unit-temporary-creation", faultAt: "symlink", wantTarget: oldTarget},
		{name: "rollback-unit-ownership", faultAt: "lchown", wantTarget: oldTarget},
		{name: "unit-rename", faultAt: "rename", wantTarget: oldTarget},
		{name: "unit-directory-sync", faultAt: "sync", wantTarget: newTarget},
	} {
		t.Run(test.name, func(t *testing.T) {
			active := filepath.Join(directory, "active-"+test.faultAt)
			_ = os.Remove(active)
			if err := os.Symlink(oldTarget, active); err != nil {
				t.Fatal(err)
			}
			ops := productionDirectoryEntryOps
			switch test.faultAt {
			case "symlink":
				ops.symlink = func(string, string) error { return fault }
			case "lchown":
				ops.lchown = func(string, int, int) error { return fault }
			case "rename":
				ops.rename = func(string, string) error { return fault }
			case "sync":
				ops.syncDir = func(string) error { return fault }
			}
			if err := replaceSymlinkDurably(ops, active, newTarget, ".transition", true, 0, 0); !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
			selected, err := os.Readlink(active)
			if err != nil || selected != test.wantTarget {
				t.Fatalf("selected=%q want=%q err=%v", selected, test.wantTarget, err)
			}
		})
	}

	marker := filepath.Join(directory, "deployment-profile")
	if err := os.WriteFile(marker, []byte("native-hardened\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeOps := productionDirectoryEntryOps
	removeOps.remove = func(string) error { return fault }
	if err := removeRegularEntryDurably(removeOps, marker); !errors.Is(err, fault) {
		t.Fatalf("marker remove fault=%v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker disappeared before remove succeeded: %v", err)
	}
	syncOps := productionDirectoryEntryOps
	syncOps.syncDir = func(string) error { return fault }
	if err := removeRegularEntryDurably(syncOps, marker); !errors.Is(err, fault) {
		t.Fatalf("marker directory-sync fault=%v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker removal did not expose complete new state: %v", err)
	}
	if err := removeRegularEntryDurably(productionDirectoryEntryOps, marker); err != nil {
		t.Fatalf("restart did not durably accept already-absent marker: %v", err)
	}
}

func TestGenerationHardenedRootCreationFaultMatrix(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	fault := errors.New("generation hardened root fault")
	for _, test := range []struct {
		name, faultAt string
		rootVisible   bool
	}{
		{name: "hardened-root-mkdir", faultAt: "mkdir"},
		{name: "hardened-root-chown", faultAt: "chown"},
		{name: "hardened-root-chmod", faultAt: "chmod"},
		{name: "hardened-root-parent-sync", faultAt: "sync", rootVisible: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "hardened")
			ops := productionDirectoryCreationOps
			switch test.faultAt {
			case "mkdir":
				ops.mkdir = func(string, os.FileMode) error { return fault }
			case "chown":
				ops.chown = func(string, int, int) error { return fault }
			case "chmod":
				ops.chmod = func(string, os.FileMode) error { return fault }
			case "sync":
				ops.syncDir = func(string) error { return fault }
			}
			if err := createOwnedDirectoryDurably(ops, root, 0o700, 0, 0); !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
			info, err := os.Stat(root)
			if test.rootVisible {
				if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
					t.Fatalf("recoverable new root info=%#v err=%v", info, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("old state was not restored after pre-publication fault: %v", err)
			}
		})
	}
}

func TestGenerationMarkerPublicationFaultMatrixReopensOldOrNewAuthority(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	directory := t.TempDir()
	marker := filepath.Join(directory, "deployment-profile")
	oldData, newData := []byte("native-legacy-root\n"), []byte("native-hardened\n")
	fault := errors.New("generation marker fault")
	for _, test := range []struct {
		faultAt string
		want    []byte
	}{
		{faultAt: "create", want: oldData},
		{faultAt: "write", want: oldData},
		{faultAt: "sync", want: oldData},
		{faultAt: "close", want: oldData},
		{faultAt: "rename", want: oldData},
		{faultAt: "sync-dir", want: newData},
	} {
		t.Run(test.faultAt, func(t *testing.T) {
			if err := atomicWrite(marker, oldData, 0o644, 0, 0); err != nil {
				t.Fatal(err)
			}
			ops := productionDeploymentAtomicOps
			ops.createTemp = func(directory, pattern string) (deploymentAtomicFile, error) {
				if test.faultAt == "create" {
					return nil, fault
				}
				file, err := os.CreateTemp(directory, pattern)
				if err != nil {
					return nil, err
				}
				return &checkpointFaultAtomicFile{File: file, faultAt: test.faultAt, fault: fault}, nil
			}
			if test.faultAt == "rename" {
				ops.rename = func(string, string) error { return fault }
			}
			if test.faultAt == "sync-dir" {
				ops.syncDir = func(string) error { return fault }
			}
			if err := atomicWriteWithOps(ops, marker, newData, 0o644, 0, 0); !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
			reopened, err := os.ReadFile(marker)
			if err != nil || string(reopened) != string(test.want) {
				t.Fatalf("reopened marker=%q want=%q err=%v", reopened, test.want, err)
			}
		})
	}
}

func TestGenerationMigrationMarkerAndHardenedRootSyncFaultMatrix(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	fault := errors.New("generation migration marker fault")
	revision := strings.Repeat("a", 64)
	for _, faultAt := range []string{"create", "write", "sync", "close", "rename", "sync-dir"} {
		t.Run(faultAt, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, ".solovey-migration-"+revision)
			ops := productionDeploymentAtomicOps
			ops.createTemp = func(directory, pattern string) (deploymentAtomicFile, error) {
				if faultAt == "create" {
					return nil, fault
				}
				file, err := os.CreateTemp(directory, pattern)
				if err != nil {
					return nil, err
				}
				return &checkpointFaultAtomicFile{File: file, faultAt: faultAt, fault: fault}, nil
			}
			if faultAt == "rename" {
				ops.rename = func(string, string) error { return fault }
			}
			if faultAt == "sync-dir" {
				ops.syncDir = func(path string) error {
					if path != root {
						t.Fatalf("hardened-root sync path=%q want=%q", path, root)
					}
					return fault
				}
			}
			if err := atomicWriteWithOps(ops, marker, []byte(revision+"\n"), 0o400, 0, 0); !errors.Is(err, fault) {
				t.Fatalf("fault result=%v", err)
			}
			data, err := os.ReadFile(marker)
			if faultAt == "sync-dir" {
				if err != nil || string(data) != revision+"\n" {
					t.Fatalf("published recoverable marker=%q err=%v", data, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("pre-publication fault exposed marker=%q err=%v", data, err)
			}
		})
	}
}

func TestGenerationStoppedSQLiteGenerationReopensExactWALFixture(t *testing.T) {
	if childPath := os.Getenv("SUI_Generation_SQLITE_CHILD"); childPath != "" {
		db, err := sql.Open("sqlite3", childPath+"?_journal_mode=WAL&_synchronous=FULL&_busy_timeout=3000")
		if err != nil || db.Ping() != nil {
			os.Exit(2)
		}
		_, _ = db.Exec("PRAGMA wal_autocheckpoint=0")
		if _, err := db.Exec("CREATE TABLE generation_rows(id INTEGER PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
			os.Exit(3)
		}
		tx, err := db.Begin()
		if err != nil {
			os.Exit(4)
		}
		for index := 0; index < 200; index++ {
			if _, err := tx.Exec("INSERT INTO generation_rows(value) VALUES (?)", fmt.Sprintf("generation-%03d", index)); err != nil {
				os.Exit(5)
			}
		}
		if err := tx.Commit(); err != nil {
			os.Exit(6)
		}
		// Model an externally stopped service: the process exits without a
		// SQLite close/checkpoint callback, leaving its coherent WAL generation.
		os.Exit(0)
	}
	requireRootDeploymentBrokerTest(t)
	sourceRoot, targetRoot := t.TempDir(), filepath.Join(t.TempDir(), "hardened")
	sourceDB := filepath.Join(sourceRoot, "solovey-ui.db")
	command := exec.Command(os.Args[0], "-test.run=^TestGenerationStoppedSQLiteGenerationReopensExactWALFixture$")
	command.Env = append(os.Environ(), "SUI_Generation_SQLITE_CHILD="+sourceDB)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("stopped SQLite fixture failed: %v output=%s", err, output)
	}
	walInfo, err := os.Stat(sourceDB + "-wal")
	if err != nil || walInfo.Size() == 0 {
		t.Fatalf("abruptly stopped fixture has no WAL generation: info=%#v err=%v", walInfo, err)
	}
	if err := os.Mkdir(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyStoppedSQLiteGeneration(sourceRoot, targetRoot, 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_, sourceErr := os.Stat(sourceDB + suffix)
		_, targetErr := os.Stat(filepath.Join(targetRoot, "solovey-ui.db") + suffix)
		if errors.Is(sourceErr, os.ErrNotExist) != errors.Is(targetErr, os.ErrNotExist) || sourceErr != nil && !errors.Is(sourceErr, os.ErrNotExist) || targetErr != nil && !errors.Is(targetErr, os.ErrNotExist) {
			t.Fatalf("sidecar %q source=%v target=%v", suffix, sourceErr, targetErr)
		}
	}
	reopened, err := sql.Open("sqlite3", filepath.Join(targetRoot, "solovey-ui.db")+"?_journal_mode=WAL&_busy_timeout=3000")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.QueryRow("SELECT COUNT(*) FROM generation_rows").Scan(&count); err != nil || count != 200 {
		t.Fatalf("reopened row count=%d err=%v", count, err)
	}
	var integrity string
	if err := reopened.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("reopened integrity=%q err=%v", integrity, err)
	}
}

func TestGenerationSQLiteGenerationRejectsEveryCopyFaultAndCrossFileChange(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	source := t.TempDir()
	for _, name := range []string{"solovey-ui.db", "solovey-ui.db-wal", "solovey-ui.db-shm"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("fixture-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fault := errors.New("generation copy fault")
	for _, failedName := range []string{"solovey-ui.db", "solovey-ui.db-wal", "solovey-ui.db-shm"} {
		t.Run("copy-"+strings.TrimPrefix(failedName, "solovey-ui.db"), func(t *testing.T) {
			target := t.TempDir()
			err := copyStoppedSQLiteGenerationWithCopy(source, target, func(from, to string) error {
				if filepath.Base(from) == failedName {
					return fault
				}
				return copyRegular(from, to, 0o600, 0, 0)
			})
			if !errors.Is(err, fault) {
				t.Fatalf("copy fault result=%v", err)
			}
		})
	}
	target := t.TempDir()
	mutated := false
	err := copyStoppedSQLiteGenerationWithCopy(source, target, func(from, to string) error {
		if err := copyRegular(from, to, 0o600, 0, 0); err != nil {
			return err
		}
		if !mutated {
			mutated = true
			file, err := os.OpenFile(filepath.Join(source, "solovey-ui.db-wal"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			_, writeErr := file.Write([]byte("changed"))
			closeErr := file.Close()
			return errors.Join(writeErr, closeErr)
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "generation changed") {
		t.Fatalf("cross-file generation change result=%v", err)
	}
}

func TestGenerationEverySQLiteGenerationFileHasDurableCopyBoundaries(t *testing.T) {
	requireRootDeploymentBrokerTest(t)
	fault := errors.New("generation SQLite publication fault")
	for _, name := range []string{"solovey-ui.db", "solovey-ui.db-wal", "solovey-ui.db-shm"} {
		for _, faultAt := range []string{"create", "copy", "sync", "close", "rename", "sync-dir"} {
			t.Run(name+"-"+faultAt, func(t *testing.T) {
				sourceRoot, targetRoot := t.TempDir(), t.TempDir()
				source := filepath.Join(sourceRoot, name)
				target := filepath.Join(targetRoot, name)
				data := []byte("coherent-" + name)
				if err := os.WriteFile(source, data, 0o600); err != nil {
					t.Fatal(err)
				}
				ops := productionDeploymentCopyOps
				copyFaultReached := false
				ops.createTemp = func(directory, pattern string) (deploymentAtomicFile, error) {
					if faultAt == "create" {
						return nil, fault
					}
					file, err := os.CreateTemp(directory, pattern)
					if err != nil {
						return nil, err
					}
					return &generationFaultCopyFile{File: file, faultAt: faultAt, fault: fault}, nil
				}
				if faultAt == "copy" {
					ops.copy = func(io.Writer, io.Reader) (int64, error) {
						copyFaultReached = true
						return 0, fault
					}
				}
				if faultAt == "rename" {
					ops.rename = func(string, string) error { return fault }
				}
				if faultAt == "sync-dir" {
					ops.syncDir = func(string) error { return fault }
				}
				err := copyRegularWithOps(ops, source, target, 0o600, 0, 0)
				if err == nil || faultAt != "copy" && !errors.Is(err, fault) || faultAt == "copy" && !copyFaultReached {
					t.Fatalf("fault result=%v copyFaultReached=%v", err, copyFaultReached)
				}
				reopened, err := os.ReadFile(target)
				if faultAt == "sync-dir" {
					if err != nil || string(reopened) != string(data) {
						t.Fatalf("published nonterminal file=%q err=%v", reopened, err)
					}
				} else if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("pre-publication fault exposed target: data=%q err=%v", reopened, err)
				}
			})
		}
	}
}

type generationFaultCopyFile struct {
	*os.File
	faultAt string
	fault   error
}

func (f *generationFaultCopyFile) Sync() error {
	if f.faultAt == "sync" {
		return f.fault
	}
	return f.File.Sync()
}

func (f *generationFaultCopyFile) Close() error {
	err := f.File.Close()
	if f.faultAt == "close" && err == nil {
		return f.fault
	}
	return err
}
