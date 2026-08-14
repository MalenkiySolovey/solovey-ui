package autohttps

import (
	"bufio"
	"net"
	"net/http"
	"testing"
)

func TestRedirectUsesConfiguredAuthorityAndRelativeRequestTarget(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	wrapped := NewAutoHttpsConn(server, "panel.example.test:8443")
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buffer := make([]byte, 1)
		_, _ = wrapped.Read(buffer)
	}()

	if _, err := client.Write([]byte("GET http://attacker.example/escaped%2Fpath?q=1 HTTP/1.1\r\nHost: attacker.example\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := response.Header.Get("Location"), "https://panel.example.test:8443/escaped%2Fpath?q=1"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	<-readDone
}

func TestRedirectRejectsUnsafeAuthority(t *testing.T) {
	for _, authority := range []string{"", "user@example.test", "example.test/path", "example.test\\path", "example.test\r\nX: injected"} {
		if authority == "" {
			continue
		}
		if validRedirectAuthority(authority) {
			t.Fatalf("unsafe redirect authority %q was accepted", authority)
		}
	}
}
