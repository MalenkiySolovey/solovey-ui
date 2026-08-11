package service

import (
	"testing"

	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
)

func TestSecurityValidateOptionalHTTPURLRejectsUnsafeSyntax(t *testing.T) {
	for _, rawURL := range []string{
		"file:///etc/passwd",
		"ftp://8.8.8.8/file",
		"socks5://8.8.8.8:1080",
		"https://user:pass@example.com/path",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := settingsvalidation.ValidateOptionalHTTPURL(rawURL); err == nil {
				t.Fatalf("expected %q to be rejected", rawURL)
			}
		})
	}
}

func TestSecurityValidateOptionalHTTPURLRejectsPrivateHosts(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1:8080",
		"http://10.0.0.1",
		"http://172.16.0.1",
		"http://192.168.1.1",
		"http://169.254.1.1",
		"http://224.0.0.1",
	} {
		if err := settingsvalidation.ValidateOptionalHTTPURL(rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}
