package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/middleware/requestbudget"
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
			"/api/inboundDrafts",
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

func TestAdminAPIRouteSecurityInventoryIsComplete(t *testing.T) {
	initSessionTestDB(t)
	prepareComponentRouteMetadata(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	apiv2 := NewAPIv2Handler(router.Group("/apiv2"))
	NewAPIHandler(router.Group("/api"), apiv2)

	registry := requestbudget.NewRegistry("")
	registry.DeclareGinRoutes(router.Routes())
	for _, route := range router.Routes() {
		if route.Path != "/api" && route.Path != "/apiv2" &&
			!strings.HasPrefix(route.Path, "/api/") &&
			!strings.HasPrefix(route.Path, "/apiv2/") {
			continue
		}
		policy, ok := registry.Lookup(route.Method, route.Path)
		if !ok {
			t.Fatalf("missing security inventory for %s %s", route.Method, route.Path)
		}
		if policy.Authentication == "" || policy.ActionScope == "" ||
			policy.BodyClass == "" || policy.PressureClass == "" ||
			policy.AuditPolicy == "" || policy.ResponseClass != "bounded_safe_envelope" {
			t.Fatalf("incomplete security inventory for %s %s: %#v", route.Method, route.Path, policy)
		}
	}

	requiredStepUp := map[string]string{
		"/api/changePass":                         "admin.credential",
		"/api/addAdmin":                           "admin.create",
		"/api/deleteAdmin":                        "admin.delete",
		"/api/addToken":                           "token.create",
		"/api/deleteToken":                        "token.revoke",
		"/api/setTokenEnabled":                    "token.change",
		"/api/importdb":                           "backup.restore",
		"/api/v1/security/password/change":        "admin.credential",
		"/api/v1/security/sessions/revoke-others": "sessions.revoke_others",
		"/api/v1/security/sessions/adopt-bounded": "sessions.adopt_bounded",
		"/api/v1/security/mfa/enroll":             "mfa.enroll",
		"/api/v1/security/mfa/recovery/rotate":    "mfa.recovery.rotate",
		"/api/v1/security/mfa/disable":            "mfa.disable",
		"/api/v1/operations/update/prepare":       "update.prepare",
		"/api/v1/operations/update/preflight":     "update.prepare",
		"/api/v1/operations/update/activate":      "update.activate",
		"/api/v1/operations/update/rollback":      "update.rollback",
		"/api/v1/operations/data/drop":            "data.drop",
	}
	for path, operation := range requiredStepUp {
		policy, ok := registry.Lookup(http.MethodPost, path)
		if !ok || policy.StepUpOperation != operation {
			t.Fatalf("step-up inventory %s=%q, want %q (present=%v)", path, policy.StepUpOperation, operation, ok)
		}
	}
	optionalStepUp := map[string]string{
		"/api/import-xui/apply":    "backup.restore",
		"/api/import-xui/rollback": "backup.restore",
	}
	for path, operation := range optionalStepUp {
		if !routeExists(router, http.MethodPost, path) {
			continue
		}
		policy, ok := registry.Lookup(http.MethodPost, path)
		if !ok || policy.StepUpOperation != operation {
			t.Fatalf("optional step-up inventory %s=%q, want %q (present=%v)", path, policy.StepUpOperation, operation, ok)
		}
	}
}
