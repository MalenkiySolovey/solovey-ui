package ssrf

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestSafeHTTPClientRejectsUnsafeRequestBeforeDial(t *testing.T) {
	client := NewHTTPClient(time.Second, "http", "https")
	response, err := client.Get("http://127.0.0.1:1/")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("safe client accepted loopback request")
	}
}

func TestSafeRedirectPolicyRejectsHTTPSDowngrade(t *testing.T) {
	policy := safeRedirectPolicy("http", "https")
	previous, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/start", nil)
	next, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/next", nil)
	err := policy(next, []*http.Request{previous})
	if err == nil {
		t.Fatal("redirect downgrade was accepted")
	}
}

func TestSafeDialRejectsLiteralBlockedAddress(t *testing.T) {
	_, err := safeDialContext(context.Background(), "tcp", "127.0.0.1:80")
	if !errors.Is(err, errBlockedDialAddress) {
		t.Fatalf("safeDialContext error = %v, want blocked address", err)
	}
}
