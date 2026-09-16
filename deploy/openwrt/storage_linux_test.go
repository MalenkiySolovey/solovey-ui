//go:build linux

package openwrt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStorageSelectionFileTrust(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "selection.json")
	if s, err := loadStorageSelection(name); err != nil || !s.IsDefault() {
		t.Fatal("missing selection changed stock behavior")
	}
	data, _ := json.Marshal(directStorageFixture())
	if err := os.WriteFile(name, data, 0o444); err != nil {
		t.Fatal(err)
	}
	s, err := loadStorageSelection(name)
	if os.Geteuid() == 0 {
		if err != nil || s != directStorageFixture() {
			t.Fatalf("trusted selection: %v", err)
		}
	} else if err == nil {
		t.Fatal("non-root-owned selection accepted")
	}
	if err := os.Chmod(name, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadStorageSelection(name); err == nil {
		t.Fatal("writable selector accepted")
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), name); err != nil {
		t.Fatal(err)
	}
	if _, err := loadStorageSelection(name); err == nil {
		t.Fatal("dangling selector silently fell back")
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("{}"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := loadStorageSelection(name); err == nil {
		t.Fatal("empty selector silently fell back")
	}
}

func TestSelectedBlockSourceCannotUseRegularFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "fake-block")
	if err := os.WriteFile(name, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env := selectedEnvironment(selectedMountFixture)
	f, err := env.Observe(env.Storage.DatabaseFolder())
	if err != nil {
		t.Fatal(err)
	}
	f.Source = name
	if err := observeBlockSource(f); err == nil {
		t.Fatal("regular file accepted as backing")
	}
}
