package frontend

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestBundleAdmissionRejectsMissingAndUntrustedInputs(t *testing.T) {
	for _, bundle := range []fs.FS{nil, fstest.MapFS{}, fstest.MapFS{"index.html": {Data: []byte("index")}},
		fstest.MapFS{"assets/main.js": {Data: []byte("asset")}},
		fstest.MapFS{"index.html/child": {Data: []byte("index")}, "assets/main.js": {Data: []byte("asset")}},
		fstest.MapFS{"index.html": {Data: []byte("index")}, "assets": {Data: []byte("file")}},
		fstest.MapFS{"index.html": {Data: []byte("outside"), Mode: fs.ModeSymlink}, "assets/main.js": {Data: []byte("asset")}}, deniedBundle{}} {
		if err := Validate(bundle); err == nil {
			t.Fatalf("untrusted bundle accepted: %T", bundle)
		}
	}
	if err := Validate(fstest.MapFS{"index.html": {Data: []byte("index")}, "assets/main.js": {Data: []byte("asset")}}); err != nil {
		t.Fatal(err)
	}
}

type deniedBundle struct{}

func (deniedBundle) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

func TestAssetRoutesServeGETAndHEADAndRejectMissingChunks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := ServeAssets(fstest.MapFS{"main.js": {Data: []byte("selected-bundle")}})
	router.GET("/panel/assets/*filepath", handler)
	router.HEAD("/panel/assets/*filepath", handler)
	router.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "index fallback") })
	for _, item := range []struct {
		method, target string
		status, body   int
	}{
		{http.MethodGet, "/panel/assets/main.js", http.StatusOK, len("selected-bundle")},
		{http.MethodHead, "/panel/assets/main.js", http.StatusOK, 0},
		{http.MethodGet, "/panel/assets/old.js", http.StatusNotFound, 0},
		{http.MethodGet, "/panel/assets/../index.html", http.StatusNotFound, 0},
		{http.MethodHead, "/panel/assets/old.js", http.StatusNotFound, 0},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(item.method, item.target, nil))
		if response.Code != item.status || response.Body.Len() != item.body {
			t.Fatalf("%s %s = %d %q", item.method, item.target, response.Code, response.Body.String())
		}
	}
}

func TestOverlayPreservesPrecedenceAdmissionErrorsAndOwnerGeneration(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "shared.js"), []byte("overlay"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "optional.js"), []byte("optional"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	available := true
	ownerError := errors.New("installed owner unavailable")
	assets := Overlay(fstest.MapFS{"shared.js": {Data: []byte("product")}}, func() ([]string, error) {
		calls++
		if !available {
			return nil, ownerError
		}
		return []string{directory}, nil
	})
	data, err := fs.ReadFile(assets, "shared.js")
	if err != nil || string(data) != "product" || calls != 0 {
		t.Fatalf("embedded precedence = %q %v %d", data, err, calls)
	}
	if _, err := assets.Open("../index.html"); !errors.Is(err, fs.ErrInvalid) || calls != 0 {
		t.Fatalf("path rejection = %v calls=%d", err, calls)
	}
	data, err = fs.ReadFile(assets, "optional.js")
	if err != nil || string(data) != "optional" {
		t.Fatalf("installed overlay = %q %v", data, err)
	}
	available = false
	if _, err := assets.Open("optional.js"); !errors.Is(err, ownerError) {
		t.Fatalf("retired owner generation accepted: %v", err)
	}
	if _, err := Overlay(fstest.MapFS{}, nil).Open("missing.js"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("absent overlay = %v", err)
	}
	if _, err := Overlay(deniedBundle{}, func() ([]string, error) { t.Fatal("overlay consulted after permission rejection"); return nil, nil }).Open("main.js"); !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
}
