package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIHandlerRegistersLegacyActionRoutesExplicitly(t *testing.T) {
	initSessionTestDB(t)
	prepareComponentRouteMetadata(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &APIHandler{}
	handler.initRouter(router.Group("/api"))

	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
		if route.Path == "/api/:postAction" || route.Path == "/api/:getAction" {
			t.Fatalf("legacy catch-all route still registered: %s %s", route.Method, route.Path)
		}
	}

	expected := map[string][]string{
		http.MethodPost: {
			"/api/login",
			"/api/changePass",
			"/api/addAdmin",
			"/api/deleteAdmin",
			"/api/save",
			"/api/restartApp",
			"/api/restartSb",
			"/api/linkConvert",
			"/api/subConvert",
			"/api/getCertPing",
			"/api/importdb",
			"/api/addToken",
			"/api/deleteToken",
			"/api/setTokenEnabled",
			"/api/logoutAllAdmins",
			"/api/logout",
			"/api/checkOutbounds",
			"/api/rotateSubSecret",
			"/api/resetTraffic",
			"/api/ip-monitor/:client/clear",
		},
		http.MethodGet: {
			"/api/csrf",
			"/api/load",
			"/api/inbounds",
			"/api/outbounds",
			"/api/endpoints",
			"/api/services",
			"/api/tls",
			"/api/clients",
			"/api/config",
			"/api/users",
			"/api/settings",
			"/api/settings/schema",
			"/api/stats",
			"/api/stats/traffic",
			"/api/status",
			"/api/failover-status",
			"/api/components",
			"/api/onlines",
			"/api/logs",
			"/api/logs/entries",
			"/api/diagnostics/report",
			"/api/diagnostics/bundle",
			"/api/changes",
			"/api/keypairs",
			"/api/getdb",
			"/api/tokens",
			"/api/singbox-config",
			"/api/checkOutbound",
			"/api/version",
			"/api/security/audit/recent",
			"/api/realtime/ws-token",
			"/api/realtime/ws",
			"/api/ip-monitor/:client",
		},
	}
	expected[http.MethodPost] = append(expected[http.MethodPost], expectedOptionalAPIPostRoutes()...)
	expected[http.MethodGet] = append(expected[http.MethodGet], expectedOptionalAPIGetRoutes()...)

	for method, paths := range expected {
		for _, path := range paths {
			if !routes[method+" "+path] {
				t.Fatalf("missing explicit route %s %s", method, path)
			}
		}
	}
}
