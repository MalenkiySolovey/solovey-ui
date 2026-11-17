package web

import (
	"bytes"
	"crypto/rand"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/service"
)

// This gate is enabled only after a real frontend build is published into the
// Product embed directory. Ordinary unit runs keep their small source fixture.
func TestEmbeddedFrontendMatchesDeclaredDistribution(t *testing.T) {
	directory := os.Getenv("SOLOVEY_FRONTEND_TEST_DIST")
	if directory == "" {
		t.Skip("requires an explicitly qualified frontend distribution")
	}
	expected := os.DirFS(directory)
	bundle := EmbeddedFrontend()
	files := make(map[string]bool)
	if err := fs.WalkDir(expected, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		want, err := fs.ReadFile(expected, name)
		if err != nil {
			return err
		}
		got, err := fs.ReadFile(bundle, name)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("embedded frontend differs from declared distribution: %s", name)
		}
		files[name] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 || !files[".vite/manifest.json"] || !files["index.html"] {
		t.Fatal("requires an actual complete Vite distribution")
	}
	if err := fs.WalkDir(bundle, ".", func(name string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && !files[name] {
			t.Errorf("unexpected embedded frontend file: %s", name)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	db := initWebRouterTestDB(t)
	const base = "/m4a/nested-panel/"
	setWebRouterSetting(t, db, "webPath", base)
	password := rand.Text()
	users := service.UserService{Runtime: service.DefaultRuntime()}
	if err := users.UpdateFirstUser("admin", password); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("users").Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(WithComponents(productComponentFixture(nil)))
	if err != nil {
		t.Fatal(err)
	}
	router, err := server.initRouter()
	if err != nil {
		t.Fatal(err)
	}
	login := performWebRouterRequest(router, base+"login")
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `content="`+base+`"`) || !strings.Contains(login.Body.String(), `<base href="`+base+`"`) || strings.Contains(login.Body.String(), "{{") {
		t.Fatalf("nested frontend entry = %d: %s", login.Code, login.Body.String())
	}
	if login.Header().Get("Cache-Control") != "no-cache, no-store, must-revalidate" {
		t.Fatalf("entry cache policy = %q", login.Header().Get("Cache-Control"))
	}
	deepLink := performWebRouterRequest(router, base+"hosts/local/clients")
	if deepLink.Code != http.StatusTemporaryRedirect || deepLink.Header().Get("Location") != base+"login" {
		t.Fatalf("unauthenticated deep link = %d %q", deepLink.Code, deepLink.Header().Get("Location"))
	}
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)
	loginRuntimeTransportFixtureAtBase(t, httpServer.Client(), httpServer.URL, password, base)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, httpServer.URL+base+"hosts/local/clients", nil)
	if err != nil {
		t.Fatal(err)
	}
	authenticated, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	authenticatedBody, err := io.ReadAll(authenticated.Body)
	_ = authenticated.Body.Close()
	if err != nil || authenticated.StatusCode != http.StatusOK || !strings.Contains(string(authenticatedBody), `<base href="`+base+`"`) {
		t.Fatalf("authenticated deep link did not serve the nested frontend: %d %v", authenticated.StatusCode, err)
	}
	count := 0
	for name := range files {
		if !strings.HasPrefix(name, "assets/") {
			continue
		}
		asset := performWebRouterRequest(router, base+name)
		want, err := fs.ReadFile(expected, name)
		if err != nil {
			t.Fatal(err)
		}
		if asset.Code != http.StatusOK || !bytes.Equal(asset.Body.Bytes(), want) {
			t.Fatalf("embedded dynamic asset %s = %d, bytes=%d want=%d", name, asset.Code, asset.Body.Len(), len(want))
		}
		if asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("asset cache policy %s = %q", name, asset.Header().Get("Cache-Control"))
		}
		count++
	}
	missing := performWebRouterRequest(router, base+"assets/m4a-missing.js")
	if missing.Code != http.StatusNotFound || strings.Contains(missing.Body.String(), "<html") {
		t.Fatalf("missing module served as an entry document: %d", missing.Code)
	}
	t.Logf("verified %d embedded files and %d served runtime assets at %s", len(files), count, base)
}
