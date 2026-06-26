package config

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCoreRestartFailureFieldsDoNotExposeRawError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	handler := Handler{RemoteIP: func(*gin.Context) string { return "203.0.113.5" }}
	rawErr := "config parse failed: Authorization: Bearer core-secret-token"

	fields := handler.coreRestartFailureFields(context, errors.New(rawErr))
	if fields["errorClass"] != "config" {
		t.Fatalf("unexpected errorClass: %q", fields["errorClass"])
	}
	for _, forbiddenKey := range []string{"reason", "error"} {
		if _, ok := fields[forbiddenKey]; ok {
			t.Fatalf("forbidden field %q leaked into Telegram payload: %#v", forbiddenKey, fields)
		}
	}
	var values []string
	for _, value := range fields {
		values = append(values, value)
	}
	joined := strings.Join(values, "\n")
	for _, forbidden := range []string{rawErr, "core-secret-token", "Authorization: Bearer"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("raw restart error leaked into Telegram payload: %#v", fields)
		}
	}
}
