//go:build !minimal

package telegram

import "testing"

func TestValidateProxyURLRejectsUnsafeOutboundTargets(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1:8080",
		"http://10.0.0.1",
		"http://172.16.0.1",
		"http://192.168.1.1",
		"http://169.254.1.1",
		"http://224.0.0.1",
		"file:///etc/passwd",
		"ftp://8.8.8.8",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := ValidateProxyURL(rawURL); err == nil {
				t.Fatalf("expected %q to be rejected", rawURL)
			}
		})
	}
}

func TestValidateProxyURLAllowsPublicProxySchemes(t *testing.T) {
	for _, rawURL := range []string{
		"http://8.8.8.8:8080",
		"https://8.8.8.8:8443",
		"socks5://8.8.8.8:1080",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := ValidateProxyURL(rawURL); err != nil {
				t.Fatalf("expected %q to be accepted: %v", rawURL, err)
			}
		})
	}
}

func TestValidateProxyURLRejectsUserInfo(t *testing.T) {
	if err := ValidateProxyURL("http://user:pass@8.8.8.8:8080"); err == nil {
		t.Fatal("expected proxy userinfo to be rejected")
	}
}
