//go:build !minimal

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestImportXUIRoutesUseSharedRegistry(t *testing.T) {
	initSessionTestDB(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	apiv2 := NewAPIv2Handler(router.Group("/apiv2"))
	NewAPIHandler(router.Group("/api"), apiv2)

	routes := map[string]gin.RouteInfo{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = route
	}
	if !routeExists(router, http.MethodPost, "/api/import-xui") {
		t.Skip("import-xui component is not registered in this profile")
	}

	for _, spec := range []struct {
		Method string
		Path   string
	}{
		{Method: http.MethodPost, Path: "/import-xui"},
		{Method: http.MethodPost, Path: "/import-xui/plan"},
		{Method: http.MethodPost, Path: "/import-xui/apply"},
		{Method: http.MethodPost, Path: "/import-xui/rollback"},
		{Method: http.MethodGet, Path: "/import-xui/reports"},
	} {
		for _, prefix := range []string{"/api", "/apiv2"} {
			key := spec.Method + " " + prefix + spec.Path
			if _, ok := routes[key]; !ok {
				t.Fatalf("missing import-xui shared route %s", key)
			}
		}
	}

	route, ok := routes[http.MethodPost+" /apiv2/import-xui"]
	if !ok {
		t.Fatal("missing explicit POST /apiv2/import-xui route")
	}
	if strings.Contains(route.Handler, "postHandler") {
		t.Fatalf("POST /apiv2/import-xui is still handled by generic postHandler: %s", route.Handler)
	}
}
