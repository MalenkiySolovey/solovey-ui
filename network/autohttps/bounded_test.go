package autohttps

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type scriptedConn struct {
	net.Conn
	input                       []byte
	final                       error
	zero                        bool
	reads                       int
	output                      bytes.Buffer
	closed                      bool
	readDeadline, writeDeadline time.Time
	deadlineError               error
}

func (c *scriptedConn) Read(buffer []byte) (int, error) {
	c.reads++
	if c.zero {
		return 0, nil
	}
	n := copy(buffer, c.input)
	c.input = c.input[n:]
	if len(c.input) == 0 {
		return n, c.final
	}
	return n, nil
}
func (c *scriptedConn) Write(buffer []byte) (int, error) { return c.output.Write(buffer) }
func (c *scriptedConn) Close() error                     { c.closed = true; return nil }
func (c *scriptedConn) SetReadDeadline(value time.Time) error {
	c.readDeadline = value
	return c.deadlineError
}
func (c *scriptedConn) SetWriteDeadline(value time.Time) error {
	c.writeDeadline = value
	return c.deadlineError
}

func TestCompleteHTTPWithBytesAndEOFGetsValidRedirect(t *testing.T) {
	for _, authority := range []string{"panel.example:8443", "[2001:db8::1]:8443", ""} {
		target := "http://attacker.example/escaped%2Fpath?q=a%26b"
		if authority == "" {
			target = "/escaped%2Fpath?q=a%26b"
		}
		fixture := &scriptedConn{input: []byte("GET " + target + " HTTP/1.1\r\nHost: safe.example\r\n\r\n"), final: io.EOF}
		wrapped := NewAutoHttpsConn(fixture, authority)
		if n, err := wrapped.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatal("complete HTTP plus EOF was not consumed as a redirect")
		}
		response, err := http.ReadResponse(bufio.NewReader(&fixture.output), nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if authority == "" {
			authority = "safe.example"
		}
		if response.Proto != "HTTP/1.1" || !response.Close || response.StatusCode != http.StatusTemporaryRedirect || response.Header.Get("Location") != "https://"+authority+"/escaped%2Fpath?q=a%26b" || !fixture.closed {
			t.Fatal("redirect protocol, authority, relative target or closure changed")
		}
	}
}

func TestNonHTTPAndPartialEOFReplayBytesBeforeError(t *testing.T) {
	for _, input := range [][]byte{{0x16, 3, 1, 0, 5, 1, 0, 0, 1, 0}, []byte("GET /unfinished"), []byte("GET / HTTP/1.1\r\nInvalid Header\r\n\r\n")} {
		fixture := &scriptedConn{input: append([]byte(nil), input...), final: io.EOF}
		got, err := io.ReadAll(NewAutoHttpsConn(fixture, "safe.example"))
		if err != nil || !bytes.Equal(got, input) || fixture.output.Len() != 0 {
			t.Fatal("partial/malformed/TLS input was dropped or redirected")
		}
	}
}

func TestInspectionBudgetAndNoProgressAreBounded(t *testing.T) {
	input := []byte("GET / HTTP/1.1\r\nX: " + strings.Repeat("x", maxInspectionBytes*2) + "\r\nHost: safe.example\r\n\r\n")
	fixture := &scriptedConn{input: append([]byte(nil), input...), final: io.EOF}
	wrapped := NewAutoHttpsConn(fixture, "safe.example").(*AutoHttpsConn)
	buffer := make([]byte, maxInspectionBytes)
	n, err := wrapped.Read(buffer)
	if err != nil || n != maxInspectionBytes || fixture.reads != maxInspectionBytes/2048 || fixture.output.Len() != 0 {
		t.Fatal("inspection read beyond its bounded budget")
	}
	rest, err := io.ReadAll(wrapped)
	if err != nil || !bytes.Equal(append(buffer[:n], rest...), input) {
		t.Fatal("oversized nonclassified input was truncated")
	}
	zero := &scriptedConn{zero: true}
	if _, err := NewAutoHttpsConn(zero).Read(make([]byte, 1)); !errors.Is(err, io.ErrNoProgress) || zero.reads != 3 {
		t.Fatal("zero-progress connection spun indefinitely")
	}
	failed := &scriptedConn{deadlineError: os.ErrDeadlineExceeded}
	conn := NewAutoHttpsConn(failed).(*AutoHttpsConn)
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) || failed.reads != 0 || !conn.readBound.IsZero() {
		t.Fatal("failed deadline setup started an unbounded read or retained a phase limit")
	}
}

func TestFragmentedHTTPOverLocalTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	joinFixture(t, done, func() { _ = listener.Close() })
	go func() {
		defer close(done)
		conn, err := NewAutoHttpsListener(listener, "panel.example:8443").Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, err = conn.Read(make([]byte, 1))
		if errors.Is(err, io.EOF) {
			err = nil
		}
		done <- err
	}()
	client, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	t.Cleanup(func() { _ = client.Close() })
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	for _, fragment := range []string{"G", "ET /fragmented?q=1 HTTP/1.1\r\n", "Host: attacker.example\r\nX-Large: ", strings.Repeat("x", 4096), "\r\n", "\r\n"} {
		if _, err := io.WriteString(client, fragment); err != nil {
			t.Fatal(err)
		}
	}
	response, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.Proto != "HTTP/1.1" || response.Header.Get("Location") != "https://panel.example:8443/fragmented?q=1" {
		t.Fatal("fragmented HTTP or large bounded header was misclassified")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TCP fixture did not finish")
	}
}

type observedDeadlines struct {
	net.Conn
	mu                        sync.Mutex
	read, write               time.Time
	readStarted, writeStarted chan struct{}
	readOnce, writeOnce       sync.Once
}

func (c *observedDeadlines) Read(buffer []byte) (int, error) {
	c.readOnce.Do(func() { close(c.readStarted) })
	return c.Conn.Read(buffer)
}
func (c *observedDeadlines) Write(buffer []byte) (int, error) {
	c.writeOnce.Do(func() { close(c.writeStarted) })
	return c.Conn.Write(buffer)
}
func (c *observedDeadlines) SetReadDeadline(value time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.read = value
	return c.Conn.SetReadDeadline(value)
}
func (c *observedDeadlines) SetWriteDeadline(value time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.write = value
	return c.Conn.SetWriteDeadline(value)
}
func (c *observedDeadlines) deadlines() (time.Time, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.read, c.write
}

func TestConcurrentCallerDeadlineSurvivesFragmentedTLSClassification(t *testing.T) {
	for _, latest := range []time.Time{{}, time.Now().Add(time.Hour), time.Now().Add(3 * time.Second)} {
		server, client := net.Pipe()
		observed := &observedDeadlines{Conn: server, readStarted: make(chan struct{}), writeStarted: make(chan struct{})}
		wrapped := NewAutoHttpsConn(observed)
		_ = wrapped.SetDeadline(time.Now().Add(time.Hour))
		done := make(chan error, 1)
		joinFixture(t, done, func() { _ = server.Close(); _ = client.Close() })
		hello := []byte{0x16, 3, 1, 0, 5, 1, 0, 0, 1, 0}
		go func() {
			defer close(done)
			buffer := make([]byte, len(hello))
			_, err := io.ReadFull(wrapped, buffer)
			if err == nil && !bytes.Equal(buffer, hello) {
				err = errors.New("TLS bytes changed")
			}
			done <- err
		}()
		awaitFixture(t, observed.readStarted)
		if err := wrapped.SetDeadline(latest); err != nil {
			t.Fatal(err)
		}
		active, _ := observed.deadlines()
		if active.IsZero() || active.After(time.Now().Add(firstReadTimeout)) {
			t.Fatal("caller update removed the inspection bound")
		}
		_ = client.SetDeadline(time.Now().Add(3 * time.Second))
		for _, fragment := range [][]byte{hello[:1], hello[1:4], hello[4:]} {
			if _, err := client.Write(fragment); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("TLS fixture did not finish")
		}
		restored, _ := observed.deadlines()
		if !restored.Equal(latest) {
			t.Fatal("classification erased the latest caller read deadline")
		}
		_ = wrapped.Close()
		_ = client.Close()
	}
}

func TestStalledRedirectWriteUsesCallerDeadlineAndRestoresIt(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	observed := &observedDeadlines{Conn: server, readStarted: make(chan struct{}), writeStarted: make(chan struct{})}
	wrapped := NewAutoHttpsConn(observed, "safe.example")
	defer wrapped.Close()
	done := make(chan error, 1)
	joinFixture(t, done, func() { _ = server.Close(); _ = client.Close() })
	go func() { defer close(done); _, err := wrapped.Read(make([]byte, 1)); done <- err }()
	if _, err := io.WriteString(client, "GET / HTTP/1.1\r\nHost: safe.example\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	awaitFixture(t, observed.writeStarted)
	_, bounded := observed.deadlines()
	if bounded.IsZero() || bounded.After(time.Now().Add(redirectWriteTimeout)) {
		t.Fatal("redirect write is unbounded")
	}
	if err := wrapped.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	_, stillBounded := observed.deadlines()
	if !stillBounded.Equal(bounded) {
		t.Fatal("clearing caller deadlines erased the active response bound")
	}
	latest := time.Now().Add(30 * time.Millisecond)
	if err := wrapped.SetWriteDeadline(latest); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatal("stalled redirect did not return its timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("stalled response pinned the connection")
	}
	_, restored := observed.deadlines()
	if !restored.Equal(latest) {
		t.Fatal("write phase erased the latest caller deadline")
	}
}

func TestSilentPeerHasDefaultInspectionTimeout(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	wrapped := NewAutoHttpsConn(server)
	defer wrapped.Close()
	done := make(chan error, 1)
	joinFixture(t, done, func() { _ = server.Close(); _ = client.Close() })
	go func() { defer close(done); _, err := wrapped.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatal("silent peer did not hit an inspection deadline")
		}
	case <-time.After(firstReadTimeout + time.Second):
		t.Fatal("default inspection exceeded its timeout")
	}
}

// Every fixture joins its worker even when an assertion fails early.
func joinFixture(t *testing.T, done <-chan error, closeAll func()) {
	t.Helper()
	t.Cleanup(func() {
		closeAll()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("fixture worker did not finish after closing connections")
		}
	})
}

func awaitFixture(t *testing.T, barrier <-chan struct{}) {
	t.Helper()
	select {
	case <-barrier:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture did not reach its I/O barrier")
	}
}

type fragmentedWriter struct{ net.Conn }

func (c fragmentedWriter) Write(buffer []byte) (int, error) {
	written := 0
	for len(buffer) > 0 {
		n, err := c.Conn.Write(buffer[:min(17, len(buffer))])
		written += n
		buffer = buffer[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrNoProgress
		}
	}
	return written, nil
}

func TestRealTLSHandshakeWithFragmentedClientHello(t *testing.T) {
	certificateFixture := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(certificateFixture.Close)
	certificate := certificateFixture.Certificate()
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	serverName := certificate.DNSNames[0]
	server, client := net.Pipe()
	wrapped := NewAutoHttpsConn(server, "panel.example")
	deadline := time.Now().Add(5 * time.Second)
	_ = wrapped.SetDeadline(deadline)
	_ = client.SetDeadline(deadline)
	done := make(chan error, 1)
	joinFixture(t, done, func() { _ = server.Close(); _ = client.Close() })
	payload := []byte("authenticated TLS application payload")
	go func() {
		defer close(done)
		defer server.Close()
		secure := tls.Server(wrapped, certificateFixture.TLS.Clone())
		err := secure.Handshake()
		if err == nil {
			buffer := make([]byte, len(payload))
			_, err = io.ReadFull(secure, buffer)
			if err == nil && !bytes.Equal(buffer, payload) {
				err = errors.New("TLS application bytes changed")
			}
			if err == nil {
				_, err = secure.Write(buffer)
			}
		}
		done <- err
	}()
	secure := tls.Client(fragmentedWriter{client}, &tls.Config{RootCAs: roots, ServerName: serverName, MinVersion: tls.VersionTLS12})
	if err := secure.Handshake(); err != nil {
		t.Fatal(err)
	}
	if _, err := secure.Write(payload); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, len(payload))
	if _, err := io.ReadFull(secure, echo); err != nil || !bytes.Equal(echo, payload) {
		t.Fatal("TLS application data did not survive classification")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPartialHTTPReturnsCachedBytesBeforeCallerTimeout(t *testing.T) {
	server, client := net.Pipe()
	wrapped := NewAutoHttpsConn(server)
	_ = wrapped.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	_ = client.SetWriteDeadline(time.Now().Add(time.Second))
	done := make(chan error, 1)
	joinFixture(t, done, func() { _ = server.Close(); _ = client.Close() })
	go func() {
		defer close(done)
		buffer := make([]byte, 8)
		n, err := wrapped.Read(buffer)
		if n != 1 || buffer[0] != 'G' || err != nil {
			done <- errors.New("partial HTTP byte was not replayed before timeout")
			return
		}
		_, err = wrapped.Read(buffer)
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			err = errors.New("partial HTTP did not return its caller timeout")
		} else {
			err = nil
		}
		done <- err
	}()
	if _, err := client.Write([]byte("G")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUnsafeAuthorityDoesNotProduceRedirect(t *testing.T) {
	for _, authority := range []string{"user@host.example", "host.example/path", "host.example?q=1", "host.example#part", "host\\example", "host\r\nInjected: value", "[broken"} {
		fixture := &scriptedConn{input: []byte("GET / HTTP/1.1\r\nHost: safe.example\r\n\r\n"), final: io.EOF}
		got, err := io.ReadAll(NewAutoHttpsConn(fixture, authority))
		if err != nil || len(got) == 0 || fixture.output.Len() != 0 || fixture.closed {
			t.Fatal("unsafe configured authority produced a redirect or consumed input")
		}
	}
}
