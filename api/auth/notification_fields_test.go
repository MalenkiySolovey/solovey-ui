package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTelegramRequestFieldsUseOnlyAllowedMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	request.Header.Set("User-Agent", "Mozilla/5.0 test agent")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	handler := Handler{RemoteIP: func(*gin.Context) string { return "203.0.113.5" }}

	fields := handler.telegramRequestFields(context)
	if len(fields) != 3 || fields["ip"] != "203.0.113.5" {
		t.Fatalf("unexpected request fields: %#v", fields)
	}
	sum := sha256.Sum256([]byte(request.UserAgent()))
	if fields["ua_hash"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected ua_hash: %q", fields["ua_hash"])
	}
	if _, err := time.Parse(time.RFC3339, fields["ts"]); err != nil {
		t.Fatalf("ts is not RFC3339: %q", fields["ts"])
	}
	for _, forbidden := range []string{"user", "username", "reason", "error"} {
		if _, ok := fields[forbidden]; ok {
			t.Fatalf("forbidden field %q leaked into Telegram payload: %#v", forbidden, fields)
		}
	}
}
