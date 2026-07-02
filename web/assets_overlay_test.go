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

func TestAssetsFSUsesEmbeddedAssetsFirst(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(root, "db"))
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(root, "components", "installed.json"))
	writeComponentPack(t, root, "telegram", map[string]string{
		"shared.js":   "component",
		"optional.js": "optional",
	})
	writeInstalledMetadata(t, root, `{
		"version": 1,
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`)

	assets := assetsFS{embedded: fstest.MapFS{
		"shared.js": {Data: []byte("embedded")},
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

func TestAssetsFSMissingFileReturnsNotExist(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(root, "db"))
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(root, "components", "missing.json"))

	assets := assetsFS{embedded: fstest.MapFS{}}
	if _, err := fs.ReadFile(assets, "missing.js"); !os.IsNotExist(err) {
		t.Fatalf("missing file error = %v, want not-exist", err)
	}
}

func TestAssetsFSReadsNewlyInstalledComponentAssets(t *testing.T) {
	root := t.TempDir()
	dbDir := filepath.Join(root, "db")
	t.Setenv("SUI_DB_FOLDER", dbDir)
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(root, "components", "installed.json"))

	assets := assetsFS{embedded: fstest.MapFS{}}
	if _, err := fs.ReadFile(assets, "optional.js"); !os.IsNotExist(err) {
		t.Fatalf("missing component asset error = %v, want not-exist", err)
	}

	telegramAssets := filepath.Join(root, "components", "telegram", "frontend", "assets")
	if err := os.MkdirAll(telegramAssets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", "telegram", "component.json"), []byte(`{"id":"telegram"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(telegramAssets, "optional.js"), []byte("installed"), 0o600); err != nil {
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

	data, err := fs.ReadFile(assets, "optional.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "installed" {
		t.Fatalf("component asset = %q, want installed", data)
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

func TestComponentFrontendAssetDirsRefreshesWhenMetadataChanges(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(root, "db"))
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(root, "components", "installed.json"))
	writeComponentPack(t, root, "telegram", map[string]string{"optional.js": "installed"})
	writeInstalledMetadata(t, root, `{
		"version": 1,
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`)

	got, err := componentFrontendAssetDirs()
	if err != nil {
		t.Fatal(err)
	}
	telegramAssets := filepath.Join(root, "components", "telegram", "frontend", "assets")
	if !reflect.DeepEqual(got, []string{telegramAssets}) {
		t.Fatalf("componentFrontendAssetDirs() = %#v, want %#v", got, []string{telegramAssets})
	}

	// A cached second call must serve the same resolved set.
	again, err := componentFrontendAssetDirs()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, got) {
		t.Fatalf("cached componentFrontendAssetDirs() = %#v, want %#v", again, got)
	}

	// Rewriting installed.json (runtime remove) must invalidate the cache;
	// pad the payload so size differs even on coarse-mtime filesystems.
	writeInstalledMetadata(t, root, `{
		"version": 1,
		"profile": "custom",
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": false}
		]
	}`)

	got, err = componentFrontendAssetDirs()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("componentFrontendAssetDirs() after removal = %#v, want empty", got)
	}
}

func writeComponentPack(t *testing.T, root string, id string, assets map[string]string) {
	t.Helper()
	assetsDir := filepath.Join(root, "components", id, "frontend", "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", id, "component.json"), []byte(`{"id":"`+id+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, content := range assets {
		if err := os.WriteFile(filepath.Join(assetsDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeInstalledMetadata(t *testing.T, root string, payload string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components", "installed.json"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}
