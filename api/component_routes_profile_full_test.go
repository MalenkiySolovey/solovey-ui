//go:build !minimal

package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/gin-gonic/gin"
)

func TestTelegramComponentRoutesPresentInFullProfile(t *testing.T) {
	initSessionTestDB(t)
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"binary": "full",
		"components": [
			{"id": "fallback-html", "delivery": "in-process", "installed": true},
			{"id": "import-xui", "delivery": "in-process", "installed": true},
			{"id": "observability-extra", "delivery": "in-process", "installed": true},
			{"id": "paid-subscriptions", "delivery": "in-process", "installed": true},
			{"id": "panel-update-ui", "delivery": "in-process", "installed": true},
			{"id": "remote-outbound-subscriptions", "delivery": "in-process", "installed": true},
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &APIHandler{}
	handler.initRouter(router.Group("/api"))

	if !routeExists(router, http.MethodGet, "/api/components/fallback-html/health") {
		t.Fatal("full profile must register GET /api/components/fallback-html/health")
	}
	if !routeExists(router, http.MethodPost, "/api/telegram/test") {
		t.Fatal("full profile must register POST /api/telegram/test")
	}
	if !routeExists(router, http.MethodGet, "/api/paidsub/status") {
		t.Fatal("full profile must register GET /api/paidsub/status")
	}
	if !routeExists(router, http.MethodGet, "/api/remote-outbound-subscriptions") {
		t.Fatal("full profile must register GET /api/remote-outbound-subscriptions")
	}
	if !routeExists(router, http.MethodPost, "/api/import-xui/plan") {
		t.Fatal("full profile must register POST /api/import-xui/plan")
	}
	if !routeExists(router, http.MethodGet, "/api/security/audit") {
		t.Fatal("full profile must register GET /api/security/audit")
	}
	if !routeExists(router, http.MethodGet, "/api/update/status") {
		t.Fatal("full profile must register GET /api/update/status")
	}
}
