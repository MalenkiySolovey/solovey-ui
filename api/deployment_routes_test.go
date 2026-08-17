package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/service"
	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
	"github.com/gin-gonic/gin"
)

func TestDeploymentRoutesRequireFullBrowserAuthentication(t *testing.T) {
	router, _ := deploymentTestRouter(t)
	for _, path := range []string{
		"/api/v1/operations/deployment/profiles",
		"/api/v1/operations/deployment/manifests",
		"/api/v1/operations/deployment/broker",
		"/api/v1/operations/deployment/status",
		"/api/v1/operations/deployment/doctor",
		"/api/v1/operations/deployment/recovery",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s unauthenticated status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestDeploymentDTORejectsUnknownRawAndCompatibilityProfiles(t *testing.T) {
	router, cookies := deploymentTestRouter(t)
	for _, body := range []string{
		`{"targetProfile":"native-hardened","acknowledged":true,"unit":"/etc/systemd/system/unsafe.service"}`,
		`{"targetProfile":"native-hardened","acknowledged":true,"rawConfiguration":"ExecStart=/bin/sh"}`,
		`{"targetProfile":"native-legacy-root","acknowledged":true}`,
		`{"targetProfile":"docker-host-unprivileged","acknowledged":true}`,
	} {
		recorder := deploymentRequest(router, cookies, http.MethodPost, "/api/v1/operations/deployment/preview", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unsafe body accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestDeploymentMigrationRequiresExactConfirmation(t *testing.T) {
	router, cookies := deploymentTestRouter(t)
	digest := strings.Repeat("a", 64)
	for _, confirmation := range []string{"migrate", "MIGRATE_TO_NATIVE_ADVANCED", "MIGRATE_TO_NATIVE_HARDENED "} {
		// Build the request explicitly: confirmation is human typed data and is
		// deliberately kept separate from the two authority revisions.
		payload, _ := json.Marshal(map[string]any{
			"targetProfile": "native-hardened", "idempotencyKey": "deployment-idem-api",
			"expectedPreviewRevision": digest, "expectedPostureRevision": digest,
			"confirmation": confirmation, "acknowledged": true,
		})
		recorder := deploymentRequest(router, cookies, http.MethodPost, "/api/v1/operations/deployment/migration", string(payload))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("confirmation %q accepted: status=%d body=%s", confirmation, recorder.Code, recorder.Body.String())
		}
	}
}

func TestDeploymentMigrationRequiresStepUpBeforeProviderCall(t *testing.T) {
	router, cookies := deploymentTestRouter(t)
	digest := strings.Repeat("a", 64)
	payload, _ := json.Marshal(map[string]any{
		"targetProfile": "native-hardened", "idempotencyKey": "deployment-idem-api-step-up",
		"expectedPreviewRevision": digest, "expectedPostureRevision": digest,
		"confirmation": "MIGRATE_TO_NATIVE_HARDENED", "acknowledged": true,
	})
	recorder := deploymentRequest(router, cookies, http.MethodPost, "/api/v1/operations/deployment/migration", string(payload))
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "step-up") {
		t.Fatalf("migration without step-up status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func deploymentTestRouter(t *testing.T) (*gin.Engine, []*http.Cookie) {
	t.Helper()
	initAPITestDB(t, filepath.Join(t.TempDir(), "deployment.db"))
	t.Cleanup(func() { closeAPITestDB(t) })
	settings := &service.SettingService{}
	router, cookies := newAuthenticatedTestRouter(t, settings, func(router *gin.Engine) {
		apiService := NewApiService()
		apiService.Deployment = deploymentservice.Shared()
		handler := &APIHandler{ApiService: apiService}
		handler.registerDeploymentRoutes(router.Group("/api"))
	})
	return router, cookies
}

func deploymentRequest(router *gin.Engine, cookies []*http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	for _, item := range cookies {
		request.AddCookie(item)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
