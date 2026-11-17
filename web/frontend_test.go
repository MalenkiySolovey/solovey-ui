package web

import (
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
)

func TestFrontendInputServesTemplatesAndAssetsFromOneBundle(t *testing.T) {
	db := initWebRouterTestDB(t)
	setWebRouterSetting(t, db, "webPath", "/")
	bundle := fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>product-frontend</title><script src=\"{{.BASE_URL}}assets/selected.js\"></script>")},
		"assets/selected.js": {Data: []byte("selected-product-bundle")},
	}
	server, err := NewServer(WithFrontend(bundle))
	if err != nil {
		t.Fatal(err)
	}
	router, err := server.initRouter()
	if err != nil {
		t.Fatal(err)
	}
	login := performWebRouterRequest(router, "/login")
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), "product-frontend") || !strings.Contains(login.Body.String(), "/assets/selected.js") {
		t.Fatalf("selected frontend = %d %q", login.Code, login.Body.String())
	}
	if login.Header().Get("Cache-Control") != "no-cache, no-store, must-revalidate" {
		t.Fatalf("entry cache policy = %q", login.Header().Get("Cache-Control"))
	}
	asset := performWebRouterRequest(router, "/assets/selected.js")
	if asset.Code != http.StatusOK || asset.Body.String() != "selected-product-bundle" {
		t.Fatalf("selected asset = %d %q", asset.Code, asset.Body.String())
	}
	if asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache policy = %q", asset.Header().Get("Cache-Control"))
	}
	missing := performWebRouterRequest(router, "/assets/missing.js")
	if missing.Code != http.StatusNotFound || strings.Contains(missing.Body.String(), "product-frontend") {
		t.Fatalf("missing chunk = %d %q", missing.Code, missing.Body.String())
	}
}

func TestFrontendInputRejectsMissingOrUntrustedBundle(t *testing.T) {
	for _, test := range []struct {
		name   string
		bundle fs.FS
	}{
		{"nil", nil},
		{"empty", fstest.MapFS{}},
		{"missing assets", fstest.MapFS{"index.html": {Data: []byte("index")}}},
		{"missing index", fstest.MapFS{"assets/main.js": {Data: []byte("asset")}}},
		{"index directory", fstest.MapFS{"index.html/child": {Data: []byte("not a template")}, "assets/main.js": {Data: []byte("asset")}}},
		{"assets file", fstest.MapFS{"index.html": {Data: []byte("index")}, "assets": {Data: []byte("not a directory")}}},
		{"index symlink", fstest.MapFS{"index.html": {Data: []byte("outside"), Mode: fs.ModeSymlink}, "assets/main.js": {Data: []byte("asset")}}},
		{"permission error", deniedFrontend{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if server, err := NewServer(WithFrontend(test.bundle)); err == nil || server != nil {
				t.Fatalf("untrusted bundle accepted: server=%v err=%v", server, err)
			}
		})
	}
}

type deniedFrontend struct{}

func (deniedFrontend) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

func TestFrontendInputPreservesInstalledComponentOverlay(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(root, "db"))
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(root, "components", "installed.json"))
	writeComponentPack(t, root, "telegram", map[string]string{"shared.js": "component", "optional.js": "optional"})
	writeInstalledMetadata(t, root, `{"version":1,"components":[{"id":"telegram","delivery":"in-process","installed":true}]}`)
	server, err := NewServer(WithFrontend(fstest.MapFS{
		"index.html":       {Data: []byte("product")},
		"assets/shared.js": {Data: []byte("product-first")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{"shared.js": "product-first", "optional.js": "optional"} {
		data, err := fs.ReadFile(server.assetsFS, name)
		if err != nil || string(data) != expected {
			t.Fatalf("asset %s = %q, %v", name, data, err)
		}
	}
	if _, err := server.assetsFS.Open("../index.html"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("path escape = %v", err)
	}
}

func TestFrontendInputRejectsInvalidTemplateBeforeServing(t *testing.T) {
	server, err := NewServer(WithFrontend(fstest.MapFS{
		"index.html":     {Data: []byte("{{untrustedFunction}}")},
		"assets/main.js": {Data: []byte("asset")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if router, err := server.initRouter(); err == nil || router != nil {
		t.Fatalf("invalid template accepted: router=%v err=%v", router, err)
	}
}
