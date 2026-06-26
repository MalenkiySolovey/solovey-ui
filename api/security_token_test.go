package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	realtimehttp "github.com/MalenkiySolovey/solovey-ui/api/realtime"
	"github.com/gin-gonic/gin"
)

func TestSecurityAPITokenFromRequestBearerAndLegacyHeader(t *testing.T) {
	withAPITokenNow(t, legacyTokenHeaderSunsetAt.Add(-time.Second))

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
			got, legacy := apiTokenFromRequest(c)
			if got != tt.wantToken || legacy != tt.wantLegacy {
				t.Fatalf("apiTokenFromRequest()=(%q,%v), want (%q,%v)", got, legacy, tt.wantToken, tt.wantLegacy)
			}
		})
	}
}

func TestSecurityAPITokenFromRequestLegacySunsetIssue34(t *testing.T) {
	withAPITokenNow(t, legacyTokenHeaderSunsetAt)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/apiv2/settings", nil)
	req.Header.Set("Token", "legacy-token")
	c.Request = req

	got, legacy := apiTokenFromRequest(c)
	if got != "" || !legacy {
		t.Fatalf("expired legacy apiTokenFromRequest()=(%q,%v), want empty token and legacy=true", got, legacy)
	}
	if !c.GetBool(legacyTokenHeaderExpiredKey) {
		t.Fatal("expired legacy token header did not set expired marker")
	}

	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	req = httptest.NewRequest(http.MethodGet, "/apiv2/settings", nil)
	req.Header.Set("Authorization", "Bearer bearer-token")
	req.Header.Set("Token", "legacy-token")
	c.Request = req

	got, legacy = apiTokenFromRequest(c)
	if got != "bearer-token" || legacy {
		t.Fatalf("bearer with expired legacy header apiTokenFromRequest()=(%q,%v), want bearer token and legacy=false", got, legacy)
	}
	if c.GetBool(legacyTokenHeaderExpiredKey) {
		t.Fatal("bearer token path should not set expired legacy marker")
	}
}

func withAPITokenNow(t *testing.T, now time.Time) {
	t.Helper()
	previous := apiTokenNow
	apiTokenNow = func() time.Time { return now }
	t.Cleanup(func() { apiTokenNow = previous })
}

func TestSecurityConsumeWSTokenDoubleSpendExpiredAndCapacity(t *testing.T) {
	_ = realtimehttp.ResetTokens()
	t.Cleanup(func() { _ = realtimehttp.ResetTokens() })

	realtimehttp.StoreToken("single-use", "admin", time.Now().Add(time.Minute))
	if user, ok := realtimehttp.ConsumeToken("single-use"); !ok || user != "admin" {
		t.Fatalf("first consume failed: user=%q ok=%v", user, ok)
	}
	if user, ok := realtimehttp.ConsumeToken("single-use"); ok || user != "" {
		t.Fatalf("second consume should fail: user=%q ok=%v", user, ok)
	}

	realtimehttp.StoreToken("expired", "admin", time.Now().Add(-time.Second))
	if user, ok := realtimehttp.ConsumeToken("expired"); ok || user != "" {
		t.Fatalf("expired consume should fail: user=%q ok=%v", user, ok)
	}
	if realtimehttp.HasToken("expired") {
		t.Fatal("expired matched token should be deleted after consume attempt")
	}

	realtimehttp.StoreToken("oldest", "admin", time.Now().Add(time.Minute))
	for i := 0; i < realtimehttp.MaxTokens; i++ {
		token := "new-" + strconv.Itoa(i)
		realtimehttp.StoreToken(token, "admin", time.Now().Add(time.Hour+time.Duration(i)*time.Second))
	}
	if realtimehttp.HasToken("oldest") {
		t.Fatal("oldest websocket token was not evicted when capacity was exceeded")
	}
	if size := realtimehttp.TokenCount(); size != realtimehttp.MaxTokens {
		t.Fatalf("unexpected websocket token capacity size %d, want %d", size, realtimehttp.MaxTokens)
	}
}
