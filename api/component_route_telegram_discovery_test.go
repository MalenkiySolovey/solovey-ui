//go:build !minimal

package api

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func performDiscoveryRequest(router *gin.Engine, path, body, token, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("Origin", "http://example.com")
	}
	if csrf != "" {
		req.Header.Set(csrfHeader, csrf)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}
func TestTelegramDiscoveryHostScopeAndAudit(t *testing.T) {
	initSessionTestDB(t)
	completeTokenOwnerResetForTest(t)
	prepareComponentRouteMetadata(t)
	registerTelegramSettingsContributionForTest(t)
	user := &service.UserService{}
	read, err := user.AddToken("admin", 0, "discovery read", "read")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := user.AddToken("admin", 0, "discovery admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewAPIv2Handler(router.Group("/apiv2"))
	if r := performDiscoveryRequest(router, "/apiv2/telegram/detect-chat", `{"token":"request-only-invalid-secret"}`, read, ""); r.Code != http.StatusForbidden {
		t.Fatalf("read scope status %d", r.Code)
	}
	for _, body := range []string{`{}`, `{"token":"` + service.StoredSecretMarker + `"}`} {
		r := performDiscoveryRequest(router, "/apiv2/telegram/detect-chat", body, admin, "")
		if r.Code != http.StatusOK {
			t.Fatalf("discovery status %d", r.Code)
		}
		var msg Msg
		if err := json.Unmarshal(r.Body.Bytes(), &msg); err != nil {
			t.Fatal(err)
		}
		obj, ok := msg.Obj.(map[string]any)
		if !msg.Success || !ok || obj["errorClass"] != "missing_token" {
			t.Fatalf("expected stored-token fallback without provider request: %#v", msg)
		}
	}
	flushAPIAudit(t)
	var events []model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "telegram_chat_discovery").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected bounded audit for each admin discovery, got %d", len(events))
	}
	for _, event := range events {
		var details map[string]any
		if err := json.Unmarshal(event.Details, &details); err != nil {
			t.Fatal(err)
		}
		if event.Actor != "admin" || event.Resource != "telegram" || len(details) != 2 || details["success"] != false {
			t.Fatalf("unexpected discovery audit %#v", event)
		}
		if _, ok := details["errorClass"]; !ok {
			t.Fatal("missing safe error class")
		}
		if strings.Contains(string(event.Details), "request-only-invalid-secret") || strings.Contains(string(event.Details), `"token"`) {
			t.Fatal("audit leaked secret material")
		}
	}
}
func TestTelegramDiscoveryBrowserScopeUsesExistingCSRF(t *testing.T) {
	settings := initSessionTestDB(t)
	router, cookies, csrf := newComponentTelegramBackupFullRouter(t, settings)
	if r := performDiscoveryRequest(router, "/api/telegram/detect-chat", `{}`, "", "", cookies...); r.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status %d", r.Code)
	}
	if r := performDiscoveryRequest(router, "/api/telegram/detect-chat", `{}`, "", csrf, cookies...); r.Code != http.StatusOK {
		t.Fatalf("accepted CSRF status %d", r.Code)
	}
}
