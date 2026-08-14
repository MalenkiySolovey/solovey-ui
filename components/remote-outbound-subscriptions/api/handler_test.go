//go:build !minimal

package remotesub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRoutesFailClosedWhenHostDependenciesAreMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{})

	request := httptest.NewRequest(http.MethodGet, "/api/remote-outbound-subscriptions", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestRemoteOutboundPayloadDecoderIsStrict(t *testing.T) {
	var payload remoteOutboundBulkGroupPayload
	if err := decodeRemoteOutboundJSON([]byte(`{"name":"group","unexpected":true}`), &payload); err == nil {
		t.Fatal("unknown payload fields should be rejected")
	}
	if err := decodeRemoteOutboundJSON([]byte(`{"name":"group"} {}`), &payload); err == nil {
		t.Fatal("multiple JSON documents should be rejected")
	}
	if err := decodeRemoteOutboundJSON([]byte(`{"name":"group"}`), &payload); err != nil || strings.TrimSpace(payload.Name) != "group" {
		t.Fatalf("valid payload = %#v, %v", payload, err)
	}
}
