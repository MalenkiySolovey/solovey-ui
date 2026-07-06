//go:build minimal

package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTelegramComponentRoutesAbsentInMinimalProfile(t *testing.T) {
	initSessionTestDB(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &APIHandler{}
	handler.initRouter(router.Group("/api"))

	if routeExists(router, http.MethodGet, "/api/components/fallback-html/health") {
		t.Fatal("minimal profile must not register GET /api/components/fallback-html/health")
	}
	if routeExists(router, http.MethodPost, "/api/telegram/test") {
		t.Fatal("minimal profile must not register POST /api/telegram/test")
	}
	if routeExists(router, http.MethodGet, "/api/paidsub/status") {
		t.Fatal("minimal profile must not register GET /api/paidsub/status")
	}
	if routeExists(router, http.MethodGet, "/api/remote-outbound-subscriptions") {
		t.Fatal("minimal profile must not register GET /api/remote-outbound-subscriptions")
	}
	if routeExists(router, http.MethodPost, "/api/import-xui/plan") {
		t.Fatal("minimal profile must not register POST /api/import-xui/plan")
	}
	if routeExists(router, http.MethodGet, "/api/security/audit") {
		t.Fatal("minimal profile must not register GET /api/security/audit")
	}
	if routeExists(router, http.MethodGet, "/api/update/status") {
		t.Fatal("minimal profile must not register GET /api/update/status")
	}
}
