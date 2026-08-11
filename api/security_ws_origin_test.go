package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	realtimehttp "github.com/MalenkiySolovey/solovey-ui/api/realtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"

	"github.com/gin-gonic/gin"
)

func TestSecurityWSOriginAllowedMatrix(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		host       string
		webDomain  string
		want       bool
		wantReason string
	}{
		{name: "host mismatch", origin: "https://evil.example", host: "panel.example", wantReason: "host_mismatch"},
		{name: "invalid scheme", origin: "file://panel.example", host: "panel.example", wantReason: "invalid_scheme"},
		{name: "invalid raw query", origin: "https://panel.example?x=1", host: "panel.example", wantReason: "invalid_origin"},
		{name: "invalid fragment", origin: "https://panel.example/#token", host: "panel.example", wantReason: "invalid_origin"},
		{name: "request host match", origin: "https://panel.example", host: "panel.example", want: true, wantReason: "request_host"},
		{name: "configured domain cannot override request authority", origin: "https://panel.example:8443", host: "other.example", webDomain: "https://panel.example:8443", wantReason: "host_mismatch"},
		{name: "ambiguous request host fails before web-domain fallback", origin: "https://panel.example", host: "panel.example,evil.example", webDomain: "panel.example", wantReason: "invalid_request_host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := realtimehttp.OriginAllowed(tt.origin, tt.host, tt.webDomain)
			if got != tt.want || reason != tt.wantReason {
				t.Fatalf("realtimehttp.OriginAllowed()=(%v,%q), want (%v,%q)", got, reason, tt.want, tt.wantReason)
			}
		})
	}
}

func TestRequestAuthorityMiddlewareRejectsAmbiguousHostBeforeAuthentication(t *testing.T) {
	initSessionTestDB(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewAPIHandler(router.Group("/api"), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/security/posture", nil)
	request.Host = "panel.example,evil.example"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "Invalid request authority") {
		t.Fatalf("ambiguous Host reached authentication: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSecurityValidateWSOriginRejectsAndAudits(t *testing.T) {
	initSessionTestDB(t)
	tests := []struct {
		name   string
		origin string
		host   string
		reason string
	}{
		{name: "host mismatch", origin: "http://evil.example", host: "panel.example", reason: "host_mismatch"},
		{name: "invalid scheme", origin: "file://panel.example", host: "panel.example", reason: "invalid_scheme"},
		{name: "invalid origin query", origin: "https://panel.example?x=1", host: "panel.example", reason: "invalid_origin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/api/realtime/ws", nil)
			req.Host = tt.host
			req.Header.Set("Origin", tt.origin)
			c.Request = req

			if (&ApiService{}).realtimeHandler().ValidateOrigin(c, "admin") {
				t.Fatal("origin should have been rejected")
			}
			if c.Writer.Status() != http.StatusForbidden {
				t.Fatalf("unexpected status %d", c.Writer.Status())
			}
			flushAPIAudit(t)
			var event model.AuditEvent
			if err := dbsqlite.DB().Where("event = ?", "ws_origin_rejected").Order("id desc").First(&event).Error; err != nil {
				t.Fatal(err)
			}
			var details map[string]any
			if err := json.Unmarshal(event.Details, &details); err != nil {
				t.Fatal(err)
			}
			if details["reason"] != tt.reason {
				t.Fatalf("unexpected audit details: %#v", details)
			}
		})
	}
}

func TestSecurityValidateWSOriginRejectsMultipleHeaderValues(t *testing.T) {
	initSessionTestDB(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "http://panel.example/api/realtime/ws", nil)
	req.Header.Add("Origin", "http://panel.example")
	req.Header.Add("Origin", "http://evil.example")
	c.Request = req

	if (&ApiService{}).realtimeHandler().ValidateOrigin(c, "admin") {
		t.Fatal("multiple Origin headers should have been rejected")
	}
	if c.Writer.Status() != http.StatusForbidden {
		t.Fatalf("unexpected status %d", c.Writer.Status())
	}
	flushAPIAudit(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "ws_origin_rejected").Order("id desc").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(event.Details), "missing_or_multiple_origin") {
		t.Fatalf("unexpected audit details: %s", event.Details)
	}
}
