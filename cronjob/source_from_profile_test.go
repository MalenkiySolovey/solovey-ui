package cronjob

import (
	"testing"

	"github.com/deposist/s-ui-x/database/importxui"
	"github.com/deposist/s-ui-x/database/importxui/source/xuihttp"
)

// TestSourceFromProfileGatesUntrustedFileSSH pins the cron-side trust gate: a
// file/ssh sync source runs only when the profile was admin-saved
// (SourceTrusted). A legacy or scoped-token-authored profile (SourceTrusted
// false) is rejected, so a pre-fix scoped-authored file/ssh profile cannot run
// with full trust.
func TestSourceFromProfileGatesUntrustedFileSSH(t *testing.T) {
	for _, typ := range []string{"file", "ssh"} {
		untrusted := importxui.SyncProfileSource{Type: typ, URL: "/tmp/x-ui.db", Host: "host", SourceTrusted: false}
		if _, err := sourceFromProfile(untrusted); err == nil {
			t.Fatalf("%s source without SourceTrusted must be rejected by cron", typ)
		}

		trusted := importxui.SyncProfileSource{Type: typ, SourceTrusted: true}
		if typ == "file" {
			trusted.URL = "/tmp/x-ui.db"
		} else {
			trusted.Host = "host"
		}
		if _, err := sourceFromProfile(trusted); err != nil {
			t.Fatalf("admin-saved %s source must build: %v", typ, err)
		}
	}
}

// TestSourceFromProfileHTTPRestrictPrivatePropagates pins the M4 cron-side http
// propagation: an untrusted-authored profile (RestrictPrivate=true) yields an
// xuihttp source with the dial-time SSRF/locality guard on; an admin profile
// (false) stays unrestricted so legitimate same-host/LAN sync keeps working.
func TestSourceFromProfileHTTPRestrictPrivatePropagates(t *testing.T) {
	for _, restrict := range []bool{true, false} {
		src, err := sourceFromProfile(importxui.SyncProfileSource{
			Type:            "xuihttp",
			BaseURL:         "http://panel.example.com",
			RestrictPrivate: restrict,
		})
		if err != nil {
			t.Fatalf("xuihttp source build error: %v", err)
		}
		httpSrc, ok := src.(xuihttp.Source)
		if !ok {
			t.Fatalf("expected xuihttp.Source, got %T", src)
		}
		if httpSrc.RestrictPrivate != restrict {
			t.Fatalf("RestrictPrivate=%v, want %v", httpSrc.RestrictPrivate, restrict)
		}
	}
}
