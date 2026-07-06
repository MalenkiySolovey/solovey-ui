//go:build !minimal

package domain

import (
	"strings"
	"testing"
)

func TestNormalizePublicPath(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "root", value: "/", want: "/"},
		{name: "plain section", value: "about", want: "/about/"},
		{name: "file", value: "/robots.txt", want: "/robots.txt"},
		{name: "reject traversal", value: "/about/../status", wantErr: true},
		{name: "absolute url", value: "https://example.com/", wantErr: true},
		{name: "query", value: "/about/?x=1", wantErr: true},
		{name: "windows path", value: `C:\temp`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizePublicPath(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePublicPath: %v", err)
			}
			if got != test.want {
				t.Fatalf("path = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReservedPublicPath(t *testing.T) {
	reserved := []string{"/app/", "/sub/"}
	for _, path := range []string{"/app/login", "/sub/client", "/api/status", "/assets/app.js", "/.well-known/acme-challenge/token"} {
		if !IsReservedPublicPath(path, reserved) {
			t.Fatalf("%s should be reserved", path)
		}
	}
	for _, path := range []string{"/", "/about/", "/robots.txt"} {
		if IsReservedPublicPath(path, reserved) {
			t.Fatalf("%s should be public", path)
		}
	}
}

func TestNormalizeRedirectTarget(t *testing.T) {
	reserved := []string{"/app/", "/sub/"}
	target, external, err := NormalizeRedirectTarget("/about", reserved)
	if err != nil {
		t.Fatalf("internal redirect target: %v", err)
	}
	if target != "/about/" || external {
		t.Fatalf("internal target = %q external=%v", target, external)
	}
	target, external, err = NormalizeRedirectTarget("https://Example.com/path?q=1", reserved)
	if err != nil {
		t.Fatalf("external redirect target: %v", err)
	}
	if target != "https://example.com/path?q=1" || !external {
		t.Fatalf("external target = %q external=%v", target, external)
	}
	for _, value := range []string{"/app/", "/api/status", "javascript:alert(1)", "https://localhost/", "https://10.0.0.1/"} {
		if _, _, err := NormalizeRedirectTarget(value, reserved); err == nil {
			t.Fatalf("target %q should be rejected", value)
		}
	}
}

func TestValidateRedirectStatus(t *testing.T) {
	if got, err := ValidateRedirectStatus(0); err != nil || got != 302 {
		t.Fatalf("default redirect status = %d, err=%v", got, err)
	}
	if _, err := ValidateRedirectStatus(303); err == nil {
		t.Fatalf("303 should not be accepted")
	}
}

func TestSanitizeBodyHTML(t *testing.T) {
	got, err := SanitizeBodyHTML(`<h2 onclick="alert(1)">Title</h2><p>Hello <strong>world</strong><script>alert(1)</script><a href="javascript:alert(1)">bad</a><a href="/about">ok</a></p>`)
	if err != nil {
		t.Fatalf("SanitizeBodyHTML: %v", err)
	}
	text := string(got)
	for _, forbidden := range []string{"script", "onclick", "javascript:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sanitized html contains %q: %s", forbidden, text)
		}
	}
	for _, want := range []string{"<h2>Title</h2>", "<strong>world</strong>", `href="/about/"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("sanitized html missing %q: %s", want, text)
		}
	}
}
