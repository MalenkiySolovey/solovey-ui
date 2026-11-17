package bearer

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSecurityAPITokenFromRequestBearerAndLegacyHeader(t *testing.T) {
	now := legacyHeaderSunsetAt.Add(-time.Second)

	tests := []struct {
		name       string
		auth       string
		legacy     string
		wantToken  string
		wantLegacy bool
	}{
		{name: "bearer", auth: "Bearer bearer-token", wantToken: "bearer-token"},
		{name: "bearer takes precedence", auth: "Bearer bearer-token", legacy: "legacy-token", wantToken: "bearer-token"},
		{name: "legacy token header", legacy: "legacy-token", wantToken: "legacy-token", wantLegacy: true},
		{name: "missing", wantToken: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodGet, "/apiv2/settings", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			if tt.legacy != "" {
				req.Header.Set("Token", tt.legacy)
			}
			c.Request = req
			got, legacy := TokenFromRequest(c, now)
			if got != tt.wantToken || legacy != tt.wantLegacy {
				t.Fatalf("apiTokenFromRequest()=(%q,%v), want (%q,%v)", got, legacy, tt.wantToken, tt.wantLegacy)
			}
		})
	}
}

func TestSecurityAPITokenFromRequestLegacySunset(t *testing.T) {
	now := legacyHeaderSunsetAt

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/apiv2/settings", nil)
	req.Header.Set("Token", "legacy-token")
	c.Request = req

	got, legacy := TokenFromRequest(c, now)
	if got != "" || !legacy {
		t.Fatalf("expired legacy apiTokenFromRequest()=(%q,%v), want empty token and legacy=true", got, legacy)
	}
	if !c.GetBool(LegacyHeaderExpiredKey) {
		t.Fatal("expired legacy token header did not set expired marker")
	}

	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	req = httptest.NewRequest(http.MethodGet, "/apiv2/settings", nil)
	req.Header.Set("Authorization", "Bearer bearer-token")
	req.Header.Set("Token", "legacy-token")
	c.Request = req

	got, legacy = TokenFromRequest(c, now)
	if got != "bearer-token" || legacy {
		t.Fatalf("bearer with expired legacy header apiTokenFromRequest()=(%q,%v), want bearer token and legacy=false", got, legacy)
	}
	if c.GetBool(LegacyHeaderExpiredKey) {
		t.Fatal("bearer token path should not set expired legacy marker")
	}
}
