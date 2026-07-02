package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
)

func TestOverlayFSUsesEmbeddedAssetsFirst(t *testing.T) {
	assets := overlayFS{roots: []fs.FS{
		fstest.MapFS{
			"shared.js": {Data: []byte("embedded")},
		},
		fstest.MapFS{
			"shared.js":   {Data: []byte("component")},
			"optional.js": {Data: []byte("optional")},
		},
	}}

	data, err := fs.ReadFile(assets, "shared.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "embedded" {
		t.Fatalf("shared.js = %q, want embedded", data)
	}

	data, err = fs.ReadFile(assets, "optional.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "optional" {
		t.Fatalf("optional.js = %q, want optional", data)
	}
}

func TestOverlayFSMissingFileReturnsNotExist(t *testing.T) {
	assets := overlayFS{roots: []fs.FS{fstest.MapFS{}}}

	if _, err := fs.ReadFile(assets, "missing.js"); !os.IsNotExist(err) {
		t.Fatalf("missing file error = %v, want not-exist", err)
	}
}

func TestComponentFrontendAssetDirsUsesInstalledMetadata(t *testing.T) {
	root := t.TempDir()
	dbDir := filepath.Join(root, "db")
	t.Setenv("SUI_DB_FOLDER", dbDir)
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(root, "components", "installed.json"))

	telegramAssets := filepath.Join(root, "components", "telegram", "frontend", "assets")
	remoteAssets := filepath.Join(root, "components", "remote-outbound-subscriptions", "frontend", "assets")
	if err := os.MkdirAll(telegramAssets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(remoteAssets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", "telegram", "component.json"), []byte(`{"id":"telegram"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", "remote-outbound-subscriptions", "component.json"), []byte(`{"id":"remote-outbound-subscriptions"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", "installed.json"), []byte(`{
		"version": 1,
		"profile": "custom",
		"binary": "full",
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true},
			{"id": "remote-outbound-subscriptions", "delivery": "in-process", "installed": false}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := componentFrontendAssetDirs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{telegramAssets}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("componentFrontendAssetDirs() = %#v, want %#v", got, want)
	}
}

func TestComponentFrontendAssetDirsRejectsInstalledMissingPack(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(root, "db"))
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(root, "components", "installed.json"))
	if err := os.MkdirAll(filepath.Join(root, "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", "installed.json"), []byte(`{
		"version": 1,
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := componentFrontendAssetDirs(); err == nil {
		t.Fatal("expected missing installed component pack to fail")
	}
}

func TestComponentFrontendAssetDirsAllowsMissingMetadata(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(root, "db"))
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(root, "components", "missing.json"))

	got, err := componentFrontendAssetDirs()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("componentFrontendAssetDirs() = %#v, want empty", got)
	}
}
