package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func TestUpdateRoutesExposeExactPlannedAliasesAndRequireAuthentication(t *testing.T) {
	router, _ := updateTestRouter(t)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/v1/operations/update/posture",
		"POST /api/v1/operations/update/preflight",
		"POST /api/v1/operations/update/activate",
		"POST /api/v1/operations/update/rollback",
		"GET /api/v1/operations/update/operations/:operationId/timeline",
	} {
		if !routes[route] {
			t.Fatalf("planned update route is absent: %s", route)
		}
	}
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/operations/update/posture", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/operations/update/preflight", bytes.NewBufferString(`{}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/operations/update/activate", bytes.NewBufferString(`{}`)),
	} {
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("unauthenticated %s %s status=%d body=%s", request.Method, request.URL.Path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestUpdateTimelineRejectsUnboundedQuery(t *testing.T) {
	router, cookies := updateTestRouter(t)
	for _, path := range []string{
		"/api/v1/operations/update/operations/update-operation:test/timeline?limit=201",
		"/api/v1/operations/update/operations/update-operation:test/timeline?limit=10&command=raw",
		"/api/v1/operations/update/status?channel=main&repository=foreign",
	} {
		recorder := updateRequest(router, cookies, http.MethodGet, path, "")
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unbounded update query accepted at %s: status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestUpdateAliasDTOsRejectUnknownFieldsAndUnboundedAuthority(t *testing.T) {
	router, cookies := updateTestRouter(t)
	digest := strings.Repeat("a", 64)
	requests := []struct {
		path string
		body string
	}{
		{"/api/v1/operations/update/preflight", `{"channel":"main","expectedSequence":1,"expectedManifestDigest":"` + digest + `","idempotencyKey":"update-test","confirmation":"PREPARE_UPDATE_1","acknowledged":true,"command":"curl evil"}`},
		{"/api/v1/operations/update/activate", `{"operationId":"../../unsafe","expectedRevision":1,"confirmation":"ACTIVATE_UPDATE_1"}`},
		{"/api/v1/operations/update/rollback", `{"operationId":"update-operation:test","expectedRevision":1,"confirmation":"ROLLBACK_UPDATE_1","path":"/etc"}`},
	}
	for _, item := range requests {
		recorder := updateRequest(router, cookies, http.MethodPost, item.path, item.body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unsafe update DTO accepted at %s: status=%d body=%s", item.path, recorder.Code, recorder.Body.String())
		}
	}
}

func updateTestRouter(t *testing.T) (*gin.Engine, []*http.Cookie) {
	t.Helper()
	initAPITestDB(t, filepath.Join(t.TempDir(), "update-api.db"))
	t.Cleanup(func() { closeAPITestDB(t) })
	router, cookies := newAuthenticatedTestRouter(t, &service.SettingService{}, func(router *gin.Engine) {
		handler := &APIHandler{ApiService: NewApiService()}
		handler.registerUpdateRoutes(router.Group("/api"))
	})
	return router, cookies
}

func updateRequest(router *gin.Engine, cookies []*http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
