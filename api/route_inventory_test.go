package api

import (
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRouteInventoryIsUnique(t *testing.T) {
	initSessionTestDB(t)
	prepareComponentRouteMetadata(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &APIHandler{}
	handler.initRouter(router.Group("/api"))

	var routes []string
	for _, route := range router.Routes() {
		if openAPIContractRoute(route.Path) {
			routes = append(routes, route.Method+" "+strings.ReplaceAll(route.Path, ":operationId", "{operationId}"))
		}
	}
	sort.Strings(routes)
	if len(routes) == 0 {
		t.Fatal("operations route inventory is empty")
	}
	for index := 1; index < len(routes); index++ {
		if routes[index] == routes[index-1] {
			t.Fatalf("duplicate operations route %q", routes[index])
		}
	}
}
