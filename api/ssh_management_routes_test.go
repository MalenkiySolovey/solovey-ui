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
	sshmanagementservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
	"github.com/gin-gonic/gin"
)

func TestSSHManagementRoutesRequireFullBrowserAuthentication(t *testing.T) {
	router, _ := sshManagementTestRouter(t)
	for _, path := range []string{"/api/v1/operations/ssh/posture", "/api/v1/operations/ssh/capabilities", "/api/v1/operations/ssh/endpoints", "/api/v1/operations/ssh/recovery", "/api/v1/operations/ssh/candidate/ssh-operation:test/timeline"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s unauthenticated status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestSSHManagementDTORejectsUnknownAndRawHostFields(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	for _, body := range []string{
		`{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED"},"acknowledged":true,"rawConfig":"PermitRootLogin yes"}`,
		`{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED","path":"/etc/ssh/sshd_config"},"acknowledged":true}`,
		`{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED","port":2222},"acknowledged":true}`,
	} {
		recorder := sshManagementRequest(router, cookies, http.MethodPost, "/api/v1/operations/ssh/preview", body)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid JSON request") {
			t.Fatalf("unsafe body accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestSSHManagementPreviewIsTruthfulWhenProductionProviderUnavailable(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	body := `{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED"},"acknowledged":true}`
	recorder := sshManagementRequest(router, cookies, http.MethodPost, "/api/v1/operations/ssh/preview", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response Msg
	if json.Unmarshal(recorder.Body.Bytes(), &response) != nil || !response.Success {
		t.Fatalf("response=%s", recorder.Body.String())
	}
	encoded, _ := json.Marshal(response.Obj)
	text := string(encoded)
	if !strings.Contains(text, `"possible":false`) || !strings.Contains(text, "production_mutation_provider_unavailable") ||
		strings.Contains(text, "/etc/ssh") || strings.Contains(text, "sshd -T") {
		t.Fatalf("preview is not truthful/safe: %s", text)
	}
}

func TestSSHCandidateMutationRequiresStepUpBeforeProviderCall(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	body := `{"policy":{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED"},"idempotencyKey":"idem:test","expectedPreviewRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedPostureRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedEndpointRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedRecoveryRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedProviderRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","endpointId":"management:ssh:test","principalId":"principal:test","authenticationClass":"publickey","acknowledged":true}`
	recorder := sshManagementRequest(router, cookies, http.MethodPost, "/api/v1/operations/ssh/candidate", body)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "step-up") {
		t.Fatalf("candidate without step-up status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSSHReconnectConfirmationRejectsRawEvidenceFields(t *testing.T) {
	router, cookies := sshManagementTestRouter(t)
	for _, body := range []string{
		`{"expectedRevision":1,"providerEvidenceRef":"/etc/ssh/sshd_config"}`,
		`{"expectedRevision":1,"providerEvidenceRef":"ssh-proof:ok","command":"sshd -T"}`,
		`{"expectedRevision":1,"providerEvidenceRef":"ssh-proof:contains space"}`,
	} {
		recorder := sshManagementRequest(router, cookies, http.MethodPost, "/api/v1/operations/ssh/candidate/ssh-operation:test/reconnect/confirm", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unsafe proof accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestSSHOperationIDsAreTypedAndBounded(t *testing.T) {
	for _, value := range []string{"", "deployment-operation:test", "ssh-operation:contains space", "ssh-operation:../../unsafe", strings.Repeat("a", 65)} {
		if safeSSHOperationID(value) {
			t.Fatalf("unsafe SSH operation ID accepted: %q", value)
		}
	}
	for _, value := range []string{"ssh-operation:test", "ssh-operation:0123456789abcdef"} {
		if !safeSSHOperationID(value) {
			t.Fatalf("valid SSH operation ID rejected: %q", value)
		}
	}
}

func sshManagementTestRouter(t *testing.T) (*gin.Engine, []*http.Cookie) {
	t.Helper()
	initAPITestDB(t, filepath.Join(t.TempDir(), "ssh-management.db"))
	t.Cleanup(func() { closeAPITestDB(t) })
	settings := &service.SettingService{}
	router, cookies := newAuthenticatedTestRouter(t, settings, func(router *gin.Engine) {
		handler := &APIHandler{ApiService: NewApiService()}
		handler.SSHManagement = sshmanagementservice.Shared()
		handler.registerSSHManagementRoutes(router.Group("/api"))
	})
	return router, cookies
}

func sshManagementRequest(router *gin.Engine, cookies []*http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	for _, item := range cookies {
		request.AddCookie(item)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
