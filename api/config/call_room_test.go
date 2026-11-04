package config

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCallRoomDeliveryAuthorizesBeforePrivateOwnerAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allowed := false
	handler := NewHandler(Deps{RequireScope: func(c *gin.Context, resource string, scopes ...string) bool {
		if resource != "callRoom" || len(scopes) != 1 || scopes[0] != "admin" {
			t.Fatal("room credential acquired a broader authorization scope")
		}
		if !allowed {
			c.AbortWithStatus(http.StatusForbidden)
		}
		return allowed
	}})
	request := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/inbound-call-room", strings.NewReader(body))
		handler.DownloadCallRoom(c)
		return w
	}
	if got := request("private-invalid-json-canary"); got.Code != http.StatusForbidden {
		t.Fatal("unauthorized request reached parsing or the room owner")
	}
	allowed = true
	if got := request(`{"id":1,"generation":"old","extra":"private-request-canary"}`); got.Code != http.StatusBadRequest || strings.Contains(got.Body.String(), "canary") {
		t.Fatal("invalid room request was accepted or echoed")
	}
	got := request(`{"id":1,"generation":"old"}`)
	if got.Code != http.StatusConflict || strings.Contains(got.Body.String(), "old") || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unavailable room was exposed or cacheable")
	}
}

func TestEndpointPeerDeliveryAuthorizesBeforePrivateOwnerAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allowed := false
	handler := NewHandler(Deps{RequireScope: func(c *gin.Context, resource string, scopes ...string) bool {
		if resource != "endpointPeerConfig" || len(scopes) != 1 || scopes[0] != "admin" {
			t.Fatal("peer credential delivery broadened its scope")
		}
		if !allowed {
			c.AbortWithStatus(http.StatusForbidden)
		}
		return allowed
	}})
	request := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/endpoint-peer-config", strings.NewReader(body))
		handler.DownloadEndpointPeerConfig(c)
		return w
	}
	if got := request("private-invalid-json-canary"); got.Code != http.StatusForbidden {
		t.Fatal("unauthorized request reached parsing or the private owner")
	}
	allowed = true
	if got := request(`{"id":1,"host":"example.test","extra":"private-request-canary"}`); got.Code != http.StatusBadRequest || strings.Contains(got.Body.String(), "canary") {
		t.Fatal("invalid request accepted or echoed")
	}
	got := request(`{"id":1,"host":"example.test"}`)
	if got.Code != http.StatusConflict || strings.Contains(got.Body.String(), "example.test") || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unavailable peer config exposed or cacheable")
	}
}

func TestUserDeviceDeliveryAuthorizesBeforePrivateOwnerAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allowed := false
	handler := NewHandler(Deps{RequireScope: func(c *gin.Context, resource string, scopes ...string) bool {
		if resource != "clientDevices" || len(scopes) != 1 || scopes[0] != "admin" {
			t.Fatal("device credential delivery broadened its scope")
		}
		if !allowed {
			c.AbortWithStatus(http.StatusForbidden)
		}
		return allowed
	}})
	request := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/client-device-config", strings.NewReader(body))
		handler.DownloadClientDeviceConfig(c)
		return w
	}
	if got := request("private-invalid-canary"); got.Code != http.StatusForbidden {
		t.Fatal("unauthorized device request reached the owner")
	}
	allowed = true
	for _, body := range []string{`{"clientId":1,"deviceId":"bad","host":"private-canary"}`, strings.Repeat(" ", 1025), `{"clientId":1,"deviceId":"0123456789abcdef0123456789abcdef","host":"example.test","extra":"private-canary"}`} {
		got := request(body)
		if got.Code != http.StatusBadRequest || strings.Contains(got.Body.String(), "canary") || got.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("invalid private request was accepted, echoed or cacheable")
		}
	}
}
