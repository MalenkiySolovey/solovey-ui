//go:build !minimal

package service

import (
	"os"
	"path/filepath"
	"testing"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
)

func TestContentCapabilitiesRejectForeignSitesAndPathEscapes(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	content := backendContent{}
	foreign, err := content.CreateAsset(2, "foreign.txt", []byte("private site data"))
	if err != nil {
		t.Fatal(err)
	}
	forged := fallbackdomain.Asset{SiteID: 1, FilePath: foreign}
	if _, err := content.ReadAsset(forged); err == nil {
		t.Fatal("read foreign asset")
	}
	if _, err := content.StageAssetRemoval(forged); err == nil {
		t.Fatal("staged foreign asset")
	}
	if err := content.RemoveCreatedAsset(1, foreign); err == nil {
		t.Fatal("removed foreign asset")
	}
	for _, name := range []string{"../escape", "nested/file", "x:y", "", "a\\b"} {
		if _, err := content.CreateAsset(1, name, []byte("x")); err == nil {
			t.Fatalf("accepted asset name %q", name)
		}
	}
	for _, version := range []string{"", "../other", "1/2", "a"} {
		if _, err := content.BeginPublication(1, version); err == nil {
			t.Fatalf("accepted revision %q", version)
		}
	}
	draft, err := content.BeginPublication(1, "123")
	if err != nil {
		t.Fatal(err)
	}
	filename, err := draft.WritePublicFile("/", []byte("home"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape", "/../escape", "/a//b/", "/a/./b/"} {
		if _, err := draft.WritePublicFile(name, []byte("x")); err == nil {
			t.Fatalf("accepted public path %q", name)
		}
	}
	if err := draft.WriteArtifact(ArtifactPart("../outside"), nil); err == nil {
		t.Fatal("accepted undeclared artifact")
	}
	publish := fallbackdomain.Publish{ID: 7, SiteID: 1, RootDir: draft.RootDir(), Version: "123"}
	row := fallbackdomain.PublishFile{PublishID: 7, FilePath: filename}
	if data, err := content.ReadPublishFile(publish, row); err != nil || string(data) != "home" {
		t.Fatalf("owned read: %q %v", data, err)
	}
	row.PublishID++
	if _, err := content.ReadPublishFile(publish, row); err == nil {
		t.Fatal("read foreign publication record")
	}
	publish.SiteID = 2
	row.PublishID = 7
	if _, err := content.ReadPublishFile(publish, row); err == nil {
		t.Fatal("read foreign publication root")
	}
	if err := content.RemovePublication(2, draft.RootDir()); err == nil {
		t.Fatal("removed foreign publication")
	}
	if err := content.RemovePublication(1, filepath.Dir(draft.RootDir())); err == nil {
		t.Fatal("removed all revisions through one-revision capability")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "private site data" {
		t.Fatalf("foreign data changed: %q %v", data, err)
	}
	if err := draft.Discard(); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateContentCompensationRetainsExactPreviousBytes(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	t.Setenv("SUI_CACHE_FOLDER", t.TempDir())
	content := backendContent{}
	original := []TemplateFile{{Relative: "manifest.json", Data: []byte("original")}, {Relative: "pages/home.html", Data: []byte("original page")}}
	change, err := content.ReplaceTemplate("example", original)
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"../escape", "/absolute", "C:/absolute", "a/../../escape", "a\\b", "a:b"} {
		if _, err := content.ReadTemplate("example", relative); err == nil {
			t.Fatalf("read path %q", relative)
		}
		if _, err := content.ReplaceTemplate("example", []TemplateFile{{Relative: relative, Data: []byte("bad")}}); err == nil {
			t.Fatalf("wrote path %q", relative)
		}
	}
	if _, err := content.ReplaceTemplate("../example", original); err == nil {
		t.Fatal("accepted foreign template root")
	}
	change, err = content.ReplaceTemplate("example", []TemplateFile{{Relative: "manifest.json", Data: []byte("replacement")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, file := range original {
		if data, err := content.ReadTemplate("example", file.Relative); err != nil || string(data) != string(file.Data) {
			t.Fatalf("rollback %s: %q %v", file.Relative, data, err)
		}
	}
	change, err = content.RemoveTemplate("example")
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Rollback(); err != nil {
		t.Fatal(err)
	}
	if data, err := content.ReadTemplate("example", "manifest.json"); err != nil || string(data) != "original" {
		t.Fatalf("delete rollback: %q %v", data, err)
	}
}
