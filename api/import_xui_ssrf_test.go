package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/deposist/s-ui-x/database/importxui"
	"github.com/deposist/s-ui-x/database/importxui/source/xuihttp"
	"github.com/gin-gonic/gin"
)

// TestRemoteImportIsUntrusted pins the S1 trust boundary: only a non-admin
// token scope is "untrusted"; a full admin session and an admin-scoped token
// stay trusted (and may reach loopback/LAN for legitimate migrations).
func TestRemoteImportIsUntrusted(t *testing.T) {
	a := &ApiService{}
	cases := []struct {
		name     string
		setScope func(c *gin.Context)
		want     bool
	}{
		{"session admin (no token scope)", func(c *gin.Context) {}, false},
		{"admin-scoped token", func(c *gin.Context) { c.Set(apiTokenScopeKey, "admin") }, false},
		{"xui_remote-scoped token", func(c *gin.Context) { c.Set(apiTokenScopeKey, "xui_remote") }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			tc.setScope(c)
			if got := a.remoteImportIsUntrusted(c); got != tc.want {
				t.Fatalf("remoteImportIsUntrusted=%v want %v", got, tc.want)
			}
		})
	}
}

// TestApiSourceFromConfigPropagatesRestrictPrivate verifies the trust decision
// reaches the xuihttp source that actually enforces it.
func TestApiSourceFromConfigPropagatesRestrictPrivate(t *testing.T) {
	for _, restrict := range []bool{true, false} {
		src, err := apiSourceFromConfig(importxui.SyncProfileSource{
			Type:    "xuihttp",
			BaseURL: "http://panel.example.com",
		}, restrict)
		if err != nil {
			t.Fatalf("apiSourceFromConfig error: %v", err)
		}
		httpSrc, ok := src.(xuihttp.Source)
		if !ok {
			t.Fatalf("expected xuihttp.Source, got %T", src)
		}
		if httpSrc.RestrictPrivate != restrict {
			t.Fatalf("RestrictPrivate=%v want %v", httpSrc.RestrictPrivate, restrict)
		}
	}
}

// TestApiSourceFromConfigRejectsLocalSourcesForScopedToken pins M3: a scoped
// (untrusted, restrictPrivate=true) caller cannot select a file or ssh source —
// the file branch otherwise dropped restrictPrivate entirely and read any local
// SQLite path. A trusted (admin, restrictPrivate=false) caller may still use them
// for legitimate same-host/LAN migrations.
func TestApiSourceFromConfigRejectsLocalSourcesForScopedToken(t *testing.T) {
	local := []importxui.SyncProfileSource{
		{Type: "file", URL: "/tmp/x-ui.db"},
		{Type: "ssh", URL: "ssh://host/x-ui.db", Host: "host"},
	}
	for _, src := range local {
		if _, err := apiSourceFromConfig(src, true); err == nil {
			t.Fatalf("scoped token must not get a %q source", src.Type)
		}
		if _, err := apiSourceFromConfig(src, false); err != nil {
			t.Fatalf("admin caller must still get a %q source: %v", src.Type, err)
		}
	}
}

// TestValidateRemoteSyncSourceSSRF covers the save-time guard that stops an
// untrusted token from storing a profile the cron job would later fetch.
func TestValidateRemoteSyncSourceSSRF(t *testing.T) {
	cases := []struct {
		name      string
		source    importxui.SyncProfileSource
		wantError bool
	}{
		{"private http rejected", importxui.SyncProfileSource{Type: "xuihttp", BaseURL: "http://10.0.0.5:2053"}, true},
		{"metadata rejected", importxui.SyncProfileSource{Type: "xuihttp", BaseURL: "http://169.254.169.254"}, true},
		// file/ssh sources cannot be confined by the SSRF/locality guard and the
		// cron job would later run them with full trust, so a scoped token must
		// not be able to save them (M3/M4).
		{"file source rejected for scoped token", importxui.SyncProfileSource{Type: "file", URL: "/tmp/x-ui.db"}, true},
		{"ssh source rejected for scoped token", importxui.SyncProfileSource{Type: "ssh", URL: "ssh://host/x-ui.db"}, true},
		{"public http allowed", importxui.SyncProfileSource{Type: "xuihttp", BaseURL: "http://1.1.1.1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRemoteSyncSourceSSRF(context.Background(), tc.source)
			if tc.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}
