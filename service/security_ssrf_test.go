package service

import (
	"testing"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
)

func TestSecurityTelegramProxyURLRejectsUnsafeOutboundTargets(t *testing.T) {
	tests := []string{
		"http://127.0.0.1:8080",
		"http://10.0.0.1",
		"http://172.16.0.1",
		"http://192.168.1.1",
		"http://169.254.1.1",
		"http://224.0.0.1",
		"file:///etc/passwd",
		"ftp://8.8.8.8",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			if err := integrationtelegram.ValidateProxyURL(rawURL); err == nil {
				t.Fatalf("expected %q to be rejected", rawURL)
			}
		})
	}
}

func TestSecurityTelegramProxyURLAllowsPublicProxySchemes(t *testing.T) {
	for _, rawURL := range []string{
		"http://8.8.8.8:8080",
		"https://8.8.8.8:8443",
		"socks5://8.8.8.8:1080",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := integrationtelegram.ValidateProxyURL(rawURL); err != nil {
				t.Fatalf("expected %q to be accepted: %v", rawURL, err)
			}
		})
	}
}

func TestSecurityTelegramProxyURLRejectsUserInfo(t *testing.T) {
	if err := integrationtelegram.ValidateProxyURL("http://user:pass@8.8.8.8:8080"); err == nil {
		t.Fatal("expected proxy userinfo to be rejected")
	}
}

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
