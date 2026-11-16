package external

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestExternalDialRejectsPrivateDNSAnswerAfterPublicValidation(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "")
	var private atomic.Bool
	var workers sync.WaitGroup
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer server.Close()
			_ = server.SetDeadline(time.Now().Add(2 * time.Second))
			var header [2]byte
			if _, err := io.ReadFull(server, header[:]); err != nil {
				return
			}
			packet := make([]byte, binary.BigEndian.Uint16(header[:]))
			if _, err := io.ReadFull(server, packet); err != nil {
				return
			}
			var query dnsmessage.Message
			if err := query.Unpack(packet); err != nil {
				t.Error(err)
				return
			}
			response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionDesired: true, RecursionAvailable: true}, Questions: query.Questions}
			for _, question := range query.Questions {
				if question.Type == dnsmessage.TypeA {
					ip := [4]byte{93, 184, 216, 34}
					if private.Load() {
						ip = [4]byte{127, 0, 0, 1}
					}
					response.Answers = append(response.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 0}, Body: &dnsmessage.AResource{A: ip}})
				}
			}
			reply, err := response.Pack()
			if err != nil {
				t.Error(err)
				return
			}
			binary.BigEndian.PutUint16(header[:], uint16(len(reply)))
			if _, err := server.Write(append(header[:], reply...)); err != nil {
				t.Error(err)
			}
		}()
		return client, nil
	}}
	t.Cleanup(func() { workers.Wait(); net.DefaultResolver = previous })
	if err := ValidateURLContext(t.Context(), "https://source.fixture.invalid/subscription"); err != nil {
		t.Fatalf("public validation fixture failed: %v", err)
	}
	private.Store(true)
	called := false
	dial := validatedExternalDial(func(context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, errors.New("unexpected execution")
	})
	conn, err := dial(t.Context(), "tcp", "source.fixture.invalid:443")
	if conn != nil {
		conn.Close()
	}
	if !errors.Is(err, errBlockedExternalAddress) || called {
		t.Fatalf("rebinding escaped dial validation: called=%t err=%v", called, err)
	}
}

type subscriptionBoundaryTransport func(*http.Request) (*http.Response, error)

func (f subscriptionBoundaryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestExternalFetchRejectsOversizedAndFailedResponses(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "")
	for _, fixture := range []struct {
		name         string
		status, size int
		message      string
	}{
		{"oversized", http.StatusOK, maxExternalSubBytes + 1, "too large"},
		{"http failure", http.StatusServiceUnavailable, 32, "status code: 503"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			closed := false
			client := &http.Client{Transport: subscriptionBoundaryTransport(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: fixture.status, Header: make(http.Header), Body: &subscriptionBoundaryBody{Reader: strings.NewReader(strings.Repeat("x", fixture.size)), closed: &closed}, Request: request}, nil
			})}
			result, err := fetchWithClientContext(t.Context(), "https://93.184.216.34/subscription", "", nil, client)
			if err == nil || !strings.Contains(err.Error(), fixture.message) || result != "" || !closed {
				t.Fatalf("failed response accepted or leaked body: bytes=%d closed=%t err=%v", len(result), closed, err)
			}
		})
	}
}

type subscriptionBoundaryBody struct {
	io.Reader
	closed *bool
}

func (b *subscriptionBoundaryBody) Close() error { *b.closed = true; return nil }

func TestExternalRedirectRejectsMetadataAndPrivateDestinations(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "")
	previous, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://93.184.216.34/source", nil)
	for _, target := range []string{"https://127.0.0.1/sub", "https://10.0.0.1/sub", "https://169.254.169.254/latest/meta-data", "https://[::1]/sub"} {
		next, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := externalRedirectPolicy(next, []*http.Request{previous}); err == nil {
			t.Fatal("redirect accepted forbidden destination", target)
		}
	}
}
