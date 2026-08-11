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
	for _, path := range []string{
		"/api/components/server-protection/status",
		"/api/components/server-protection/resources",
		"/api/components/server-protection/host-surfaces",
		"/api/components/server-protection/target-capabilities",
		"/api/components/server-protection/signals",
		"/api/components/server-protection/decisions",
		"/api/components/server-protection/posture",
		"/api/components/server-protection/firewall-baseline",
		"/api/components/server-protection/native-fallback/status",
	} {
		if routeExists(router, http.MethodGet, path) {
			t.Fatalf("minimal profile must not register server-protection route %s", path)
		}
	}
	if routeExists(router, http.MethodPost, "/api/components/server-protection/decisions/resolve-preview") {
		t.Fatal("minimal profile must not register firewall baseline resolver preview")
	}
	for _, path := range []string{
		"/api/components/server-protection/native-fallback/preview",
		"/api/components/server-protection/native-fallback/prepare",
		"/api/components/server-protection/native-fallback/apply",
		"/api/components/server-protection/native-fallback/rollback",
	} {
		if routeExists(router, http.MethodPost, path) {
			t.Fatalf("minimal profile must not register native fallback route %s", path)
		}
	}
}
