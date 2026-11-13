//go:build !minimal

package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

type closeObservedListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (listener *closeObservedListener) Close() error {
	err := listener.Listener.Close()
	listener.once.Do(func() { close(listener.closed) })
	return err
}

func TestHTTPListenerCompensationClosesSocketAndJoinsDelayedWorker(t *testing.T) {
	for _, cancelDuringWait := range []bool{false, true} {
		name := "complete"
		if cancelDuringWait {
			name = "cancel-and-retry"
		}
		t.Run(name, func(t *testing.T) {
			socket, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			listener := &closeObservedListener{Listener: socket, closed: make(chan struct{})}
			state := &runtimeListener{server: &http.Server{ReadHeaderTimeout: time.Second}, listener: listener, done: make(chan struct{})}
			start := make(chan struct{})
			var startOnce sync.Once
			release := func() { startOnce.Do(func() { close(start) }) }
			served := make(chan error, 1)
			go func() {
				defer close(state.done)
				<-start
				served <- state.server.Serve(listener)
			}()
			t.Cleanup(func() {
				release()
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cleanupCancel()
				if err := state.closeAndWait(cleanupCtx); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- state.closeAndWait(ctx) }()
			select {
			case <-listener.closed:
			case <-ctx.Done():
				t.Fatal("compensation did not close the acquired socket before Serve")
			}
			probe, err := net.Listen("tcp", socket.Addr().String())
			if err != nil {
				t.Fatalf("compensation retained the socket before worker startup: %v", err)
			}
			if err := probe.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-stopped:
				t.Fatalf("compensation returned before joining its worker: %v", err)
			default:
			}
			if cancelDuringWait {
				cancel()
				if err := <-stopped; !errors.Is(err, context.Canceled) {
					t.Fatalf("interrupted compensation reported success: %v", err)
				}
			}
			release()
			if !cancelDuringWait {
				if err := <-stopped; err != nil {
					t.Fatal(err)
				}
			}
			if err := state.closeAndWait(t.Context()); err != nil {
				t.Fatalf("compensation retry: %v", err)
			}
			if err := <-served; !errors.Is(err, http.ErrServerClosed) {
				t.Fatalf("closed listener was admitted by Serve: %v", err)
			}
		})
	}
}
