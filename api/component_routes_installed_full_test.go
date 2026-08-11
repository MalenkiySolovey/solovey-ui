//go:build !minimal

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"net/http/httptest"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
)

func TestComponentRoutesRespectInstalledMetadata(t *testing.T) {
	initSessionTestDB(t)
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"binary": "full",
		"components": [
			{"id": "fallback-html", "delivery": "in-process", "installed": true},
			{"id": "telegram", "delivery": "in-process", "installed": true},
			{"id": "server-protection", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &APIHandler{}
	handler.initRouter(router.Group("/api"))

	if !routeExists(router, http.MethodPost, "/api/telegram/test") {
		t.Fatal("installed telegram component route is missing")
	}
	if !routeExists(router, http.MethodGet, "/api/components/fallback-html/health") {
		t.Fatal("installed fallback-html component route is missing")
	}
	if !routeExists(router, http.MethodGet, "/api/components/server-protection/status") ||
		!routeExists(router, http.MethodPost, "/api/components/server-protection/firewall/preview") ||
		!routeExists(router, http.MethodPost, "/api/components/server-protection/fronting/sync") ||
		!routeExists(router, http.MethodGet, "/api/components/server-protection/fronting/operations/:operationId") ||
		!routeExists(router, http.MethodGet, "/api/components/server-protection/native-fallback/status") ||
		!routeExists(router, http.MethodPost, "/api/components/server-protection/native-fallback/preview") ||
		!routeExists(router, http.MethodPost, "/api/components/server-protection/native-fallback/prepare") ||
		!routeExists(router, http.MethodPost, "/api/components/server-protection/native-fallback/apply") ||
		!routeExists(router, http.MethodPost, "/api/components/server-protection/native-fallback/rollback") {
		t.Fatal("installed server-protection component route snapshot is incomplete")
	}
	if routeExists(router, http.MethodGet, "/api/paidsub/status") {
		t.Fatal("paid-subscriptions route must be absent when component is not installed")
	}
	if routeExists(router, http.MethodGet, "/api/remote-outbound-subscriptions") {
		t.Fatal("remote-outbound-subscriptions route must be absent when component is not installed")
	}
}

func TestComponentRoutesRespectEnabledSetting(t *testing.T) {
	initSessionTestDB(t)
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"binary": "full",
		"components": [
			{"id": "fallback-html", "delivery": "in-process", "installed": true},
			{"id": "telegram", "delivery": "in-process", "installed": true},
			{"id": "paid-subscriptions", "delivery": "in-process", "installed": true},
			{"id": "server-protection", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: enabledstate.SettingKey("telegram"), Value: "false"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: enabledstate.SettingKey("fallback-html"), Value: "false"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: enabledstate.SettingKey("server-protection"), Value: "false"}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &APIHandler{}
	handler.initRouter(router.Group("/api"))

	if !routeExists(router, http.MethodPost, "/api/telegram/test") {
		t.Fatal("installed disabled telegram component route should remain registered")
	}
	if !routeExists(router, http.MethodGet, "/api/paidsub/status") {
		t.Fatal("unrelated enabled component route should remain registered")
	}
	if !routeExists(router, http.MethodGet, "/api/components/fallback-html/health") {
		t.Fatal("installed disabled fallback-html component route should remain registered")
	}
	if !routeExists(router, http.MethodGet, "/api/components/server-protection/status") {
		t.Fatal("installed disabled server-protection route should remain registered")
	}

	bareRouter := gin.New()
	registerComponentAPIRoutes(bareRouter.Group("/api"), componenthost.APIDeps{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/telegram/test", strings.NewReader(`{}`))
	bareRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("disabled component route status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/components/fallback-html/health", nil)
	bareRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("disabled fallback-html route status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/components/server-protection/status", nil)
	bareRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("disabled server-protection route status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/components/server-protection/native-fallback/apply", strings.NewReader(`{}`))
	bareRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("disabled native fallback route status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestComponentStatusEndpointReturnsRuntimeInventoryOnly(t *testing.T) {
	initSessionTestDB(t)
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"binary": "full",
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &APIHandler{}
	handler.registerComponentStatusRoutes(router.Group("/api"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/components", nil))

	var response struct {
		Success bool                       `json:"success"`
		Obj     map[string]json.RawMessage `json:"obj"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success {
		t.Fatalf("response must be successful: %s", recorder.Body.String())
	}
	for _, catalogField := range []string{"installed", "available", "unavailable"} {
		if _, ok := response.Obj[catalogField]; ok {
			t.Fatalf("core runtime inventory must not expose catalog field %q: %s", catalogField, recorder.Body.String())
		}
	}
	var components []struct {
		ID        string `json:"id"`
		Installed bool   `json:"installed"`
		Active    bool   `json:"active"`
	}
	if err := json.Unmarshal(response.Obj["components"], &components); err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || components[0].ID != "telegram" || !components[0].Installed || !components[0].Active {
		t.Fatalf("components = %#v, want installed active telegram only", components)
	}
}
