package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/hostcontext"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
)

func TestHostContextAllAPICompositionsRejectForeignHost(t *testing.T) {
	settings := initSessionTestDB(t)
	router := newSecurityCSRFTestRouter(t, settings)
	NewAPIv2Handler(router.Group("/apiv2"))
	for _, path := range []string{"/api/csrf", "/api/importdb", "/api/save", "/apiv2/importdb", "/apiv2/save", "/apiv2/clients"} {
		for _, values := range [][]string{{"unavailable-example"}, {""}, {"local", "local"}, {"local", "other"}, {"local,other"}, {"LOCAL"}, {" local"}, {"local;other"}} {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			if path == "/api/csrf" || path == "/apiv2/clients" {
				request.Method = http.MethodGet
			}
			for _, value := range values {
				request.Header.Add(hostcontext.Header, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s Host %q status=%d body=%s", path, values, response.Code, response.Body)
			}
			if response.Header().Get(hostcontext.Header) != "" || response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("%s rejected Host %q changed response authority: %v", path, values, response.Header())
			}
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

func TestHostContextRejectionPrecedesDatabaseAdmission(t *testing.T) {
	settings := initSessionTestDB(t)
	router := newSecurityCSRFTestRouter(t, settings)
	NewAPIv2Handler(router.Group("/apiv2"))

	owner, err := dbsqlite.BeginMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.End()

	for _, path := range []string{"/api/save", "/apiv2/save"} {
		for _, values := range [][]string{nil, {"local"}, {""}, {"other"}, {"local", "local"}, {"local,other"}} {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			for _, value := range values {
				request.Header.Add(hostcontext.Header, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := http.StatusNotFound
			if len(values) == 0 || (len(values) == 1 && values[0] == "local") {
				want = http.StatusServiceUnavailable
			}
			if response.Code != want {
				t.Fatalf("%s Host %q status=%d, want %d; body=%s", path, values, response.Code, want, response.Body)
			}
		}
	}
}
