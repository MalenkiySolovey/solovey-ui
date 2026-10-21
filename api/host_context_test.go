package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/middleware/hostcontext"
)

func TestHostContextAllAPICompositionsRejectForeignHost(t *testing.T) {
	settings := initSessionTestDB(t)
	router := newSecurityCSRFTestRouter(t, settings)
	NewAPIv2Handler(router.Group("/apiv2"))
	for _, path := range []string{"/api/csrf", "/api/importdb", "/api/save", "/apiv2/importdb", "/apiv2/save", "/apiv2/clients"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		if path == "/api/csrf" || path == "/apiv2/clients" {
			request.Method = http.MethodGet
		}
		request.Header.Set(hostcontext.Header, "unavailable-example")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s foreign Host status=%d body=%s", path, response.Code, response.Body)
		}
	}
	for _, host := range []string{"", "local"} {
		request := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
		if host != "" {
			request.Header.Set(hostcontext.Header, host)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get(hostcontext.Header) != "local" {
			t.Fatalf("Local compatibility: %d %s", response.Code, response.Body)
		}
	}
}
