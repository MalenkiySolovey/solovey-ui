package service

import "testing"

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
			if err := validateTelegramProxyURL(rawURL); err == nil {
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
			if err := validateTelegramProxyURL(rawURL); err != nil {
				t.Fatalf("expected %q to be accepted: %v", rawURL, err)
			}
		})
	}
}

func TestSecurityTelegramProxyURLRejectsUserInfo_XFAILPhase4(t *testing.T) {
	t.Skip("XFAIL Phase4: Telegram proxy URLs currently support userinfo for proxy auth; decide contract before enforcing rejection")
	if err := validateTelegramProxyURL("http://user:pass@8.8.8.8:8080"); err == nil {
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
			if err := validateOptionalHTTPURL(rawURL); err == nil {
				t.Fatalf("expected %q to be rejected", rawURL)
			}
		})
	}
}

func TestSecurityValidateOptionalHTTPURLRejectsPrivateHosts_XFAILIssue30(t *testing.T) {
	t.Skip("XFAIL Phase4: validateOptionalHTTPURL currently checks syntax/userinfo only and does not call SSRF validator; related to registry issue 30")
	for _, rawURL := range []string{
		"http://127.0.0.1:8080",
		"http://10.0.0.1",
		"http://172.16.0.1",
		"http://192.168.1.1",
		"http://169.254.1.1",
		"http://224.0.0.1",
	} {
		if err := validateOptionalHTTPURL(rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}
