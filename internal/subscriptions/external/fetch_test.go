package external

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchContextHonorsPreCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchContext(ctx, "https://example.com/subscription"); err == nil {
		t.Fatal("pre-canceled subscription fetch should fail")
	}
}

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

func TestFetchRedirectDoesNotLeakSourceCredentialsOrReferer(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "" {
			t.Fatalf("redirect target received Referer %q", got)
		}
		for _, name := range []string{"Authorization", "Cookie", "X-Private-Token", "X-Scopes"} {
			if r.Header.Get(name) != "" {
				t.Errorf("redirect target received a private source header: %s", name)
			}
		}
		_, _ = w.Write([]byte("vless://user@example.com:443#node"))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-source-canary" || r.Header.Get("X-Private-Token") != "private-source-canary" || len(r.Header.Values("X-Scopes")) != 2 {
			t.Error("original source did not receive its selected request headers")
		}
		http.Redirect(w, r, target.URL+"/sub", http.StatusFound)
	}))
	defer source.Close()

	headers := http.Header{"authorization": {"Bearer private-source-canary"}, "Cookie": {"key=private-source-canary"}, "X-Private-Token": {"private-source-canary"}, "X-Scopes": {"a", "b"}}
	data, err := FetchWithHeadersContext(context.Background(), source.URL+"/sub?token=secret", "", headers)
	if err != nil {
		t.Fatal(err)
	}
	if data == "" {
		t.Fatal("expected redirected subscription body")
	}
	if headers.Get("Authorization") != "" || len(map[string][]string(headers)["authorization"]) != 1 {
		t.Fatal("request normalized headers by mutating the caller's captured values")
	}
}

func TestFetchHeadersRejectFramingInjectionAndCaseAliases(t *testing.T) {
	for _, headers := range []http.Header{
		{"Host": {"source.example"}}, {"Content-Length": {"1"}}, {"User-Agent": {"other"}},
		{"Connection": {"keep-alive"}}, {"Proxy-Authorization": {"private-canary"}},
		{"X-Key": {"private-canary\r\nHost: other"}}, {"bad name": {"private-canary"}},
		{"X-Key": {"one"}, "x-key": {"two"}}, {"X-Key": {}},
	} {
		if err := ValidateFetchHeaders(headers); !errors.Is(err, ErrFetchHeadersInvalid) {
			t.Fatal("invalid private request headers were accepted", err)
		}
	}
}
