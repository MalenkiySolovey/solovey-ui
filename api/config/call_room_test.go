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
