package privilegedbroker

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestClientCancellationClosesConnectedResponseWait(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	t.Cleanup(func() { _ = clientSide.Close(); _ = serverSide.Close() })
	requestRead, serverDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(serverDone)
		var request Request
		if ReadFrame(serverSide, &request, MaxRequestBytes) == nil {
			close(requestRead)
		}
		_, _ = io.Copy(io.Discard, serverSide)
	}()
	client := &Client{Role: RolePanel, BootID: "fixture-boot", Dial: func(context.Context, string) (net.Conn, error) { return clientSide, nil }}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.Invoke(ctx, Call{Verb: VerbCapabilities, OperationID: "cancellation-fixture", Timeout: 2 * time.Minute, Payload: struct{}{}}, nil)
		done <- err
	}()
	select {
	case <-requestRead:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach the connected broker")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled request acquired a receipt")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("connected broker ignored caller cancellation")
	}
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled request left its connection worker alive")
	}
}
