package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestNewServerInitializesEmbeddedAssets(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	if server == nil || server.assetsFS == nil {
		t.Fatal("expected server with embedded assets filesystem")
	}
}

func TestNoRouteDelegatesOutsideAdminBaseToPublicSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := initWebRouterTestDB(t)
	setWebRouterSetting(t, db, "webPath", "/secret-panel/")
	unregister := publicsurface.Register("web-test-public", webTestPublicHandler{})
	t.Cleanup(unregister)

	server, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	router, err := server.initRouter()
	if err != nil {
		t.Fatalf("initRouter: %v", err)
	}

	public := performWebRouterRequest(router, "/public")
	if public.Code != http.StatusOK || public.Body.String() != "public" {
		t.Fatalf("public response = %d %q", public.Code, public.Body.String())
	}
	if location := public.Header().Get("Location"); location != "" {
		t.Fatalf("public route must not redirect to login, Location=%q", location)
	}
	if public.Header().Get("X-Frame-Options") != "" {
		t.Fatalf("public route inherited admin X-Frame-Options: %#v", public.Header())
	}
	if csp := public.Header().Get("Content-Security-Policy"); csp != "" {
		t.Fatalf("public route inherited admin CSP: %q", csp)
	}
	if public.Header().Get("Strict-Transport-Security") != "" {
		t.Fatalf("public route inherited admin HSTS: %#v", public.Header())
	}

	missing := performWebRouterRequest(router, "/not-public")
	if missing.Code != http.StatusNotFound || missing.Header().Get("Location") != "" {
		t.Fatalf("missing public route = %d Location=%q", missing.Code, missing.Header().Get("Location"))
	}

	acme := performWebRouterRequest(router, "/.well-known/acme-challenge/token")
	if acme.Code != http.StatusNotFound || acme.Header().Get("Location") != "" {
		t.Fatalf("ACME fallback = %d Location=%q", acme.Code, acme.Header().Get("Location"))
	}

	admin := performWebRouterRequest(router, "/secret-panel/unknown")
	if admin.Code != http.StatusTemporaryRedirect || !strings.HasSuffix(admin.Header().Get("Location"), "/secret-panel/login") {
		t.Fatalf("admin route = %d Location=%q", admin.Code, admin.Header().Get("Location"))
	}
	if admin.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("admin route lost admin X-Frame-Options: %#v", admin.Header())
	}
	if csp := admin.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("admin route lost admin CSP: %q", csp)
	}
}

type webTestPublicHandler struct{}

func (webTestPublicHandler) ServePublic(c *gin.Context, _ publicsurface.Context) bool {
	if c.Request.URL.Path != "/public" {
		return false
	}
	c.String(http.StatusOK, "public")
	return true
}

func initWebRouterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(dir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	return dbsqlite.DB()
}

func setWebRouterSetting(t *testing.T, db *gorm.DB, key string, value string) {
	t.Helper()
	if err := db.Where("key = ?", key).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("delete setting %s: %v", key, err)
	}
	if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
		t.Fatalf("create setting %s: %v", key, err)
	}
}

func performWebRouterRequest(router http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(response, request)
	return response
}
