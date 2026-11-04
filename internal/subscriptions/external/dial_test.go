package external

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOutboundFetchKeepsDestinationBoundaryAndNeverFallsBack(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "")
	calls := 0
	blocked := errors.New("selected outbound stopped")
	dial := func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, blocked
	}
	if _, err := validatedExternalDial(dial)(context.Background(), "tcp", "127.0.0.1:80"); !errors.Is(err, errBlockedExternalAddress) || calls != 0 {
		t.Fatal("outbound bypassed private-address validation", err)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: dial}}
	if _, err := FetchWithOutboundContext(context.Background(), "https://8.8.8.8/sub", "", nil, client); !errors.Is(err, blocked) || calls != 1 {
		t.Fatal("selected dial authority was ignored or replaced with a direct fallback", err, calls)
	}
	if _, err := FetchWithOutboundContext(context.Background(), "https://8.8.8.8/sub", "", nil, &http.Client{}); err == nil {
		t.Fatal("missing outbound transport became the default transport")
	}
}

// TestExternalHTTPClientDialerBlocksLoopback verifies the dialer hook rejects
// connections that resolve to private addresses, even when the URL passed
// validation. This guards against DNS-rebinding attacks.
func TestExternalHTTPClientDialerBlocksLoopback(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "")

	srv := httptest.NewServer(nil)
	defer srv.Close()

	transport, ok := getExternalHTTPClient().Transport.(*http.Transport)
	if !ok || transport.DialContext == nil {
		t.Fatalf("expected configured *http.Transport with DialContext")
	}
	addr := strings.TrimPrefix(srv.URL, "http://")
	_, err := transport.DialContext(context.Background(), "tcp", addr)
	if err == nil {
		t.Fatal("expected dialer to reject loopback address")
	}
	if !errors.Is(err, errBlockedExternalAddress) {
		t.Fatalf("expected errBlockedExternalAddress, got %v", err)
	}
}

func TestExternalHTTPClientDialerAllowsWhenOptedIn(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "true")

	srv := httptest.NewServer(nil)
	defer srv.Close()

	transport, ok := getExternalHTTPClient().Transport.(*http.Transport)
	if !ok || transport.DialContext == nil {
		t.Fatalf("expected configured *http.Transport with DialContext")
	}
	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := transport.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("expected dialer to allow loopback when opted-in, got %v", err)
	}
	conn.Close()
}

func TestExternalHTTPClientDoesNotUseEnvironmentProxy(t *testing.T) {
	client := newExternalHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected configured *http.Transport")
	}
	if transport.Proxy != nil {
		t.Fatal("untrusted subscription fetch must resolve and dial its target directly")
	}
}
