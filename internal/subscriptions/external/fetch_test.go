package external

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateExternalURLRejectsUnsafeTargets(t *testing.T) {
	tests := []string{
		"file:///tmp/sub.txt",
		"http://localhost/sub.txt",
		"http://127.0.0.1/sub.txt",
		"http://10.0.0.1/sub.txt",
		"http://100.64.0.1/sub.txt",
		"http://192.0.2.1/sub.txt",
		"http://198.18.0.1/sub.txt",
		"http://240.0.0.1/sub.txt",
		"http://[::1]/sub.txt",
		"http://[::ffff:127.0.0.1]/sub.txt",
		"http://[2001:db8::1]/sub.txt",
	}
	for _, rawURL := range tests {
		if err := ValidateURL(rawURL); err == nil {
			t.Fatalf("expected %s to be rejected", rawURL)
		}
	}
}

func TestValidateExternalURLAllowsPrivateTargetsWhenExplicitlyEnabled(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	if err := ValidateURL("http://127.0.0.1/sub.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestFetchRedirectDoesNotLeakSubscriptionURLInReferer(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "" {
			t.Fatalf("redirect target received Referer %q", got)
		}
		_, _ = w.Write([]byte("vless://user@example.com:443#node"))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/sub", http.StatusFound)
	}))
	defer source.Close()

	data, err := Fetch(source.URL + "/sub?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	if data == "" {
		t.Fatal("expected redirected subscription body")
	}
}
