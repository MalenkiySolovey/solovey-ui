//go:build linux

package privilegedbroker

import (
	"context"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type signalingRevisionAttestor struct {
	revisionAttestor
	once  *sync.Once
	ready chan<- struct{}
}

func (a signalingRevisionAttestor) Attest(ctx context.Context, connection *net.UnixConn, role Role) (PeerIdentity, error) {
	peer, err := a.revisionAttestor.Attest(ctx, connection, role)
	if err == nil {
		a.once.Do(func() { close(a.ready) })
	}
	return peer, err
}

func TestOpenConnectionCannotSurviveAuthorityGenerationRotation(t *testing.T) {
	first := Digest([]byte("runtime-authority-first"))
	second := Digest([]byte("runtime-authority-second"))
	ready := make(chan struct{})
	initial := signalingRevisionAttestor{
		revisionAttestor: revisionAttestor{revision: first, peer: PeerIdentity{BootID: "boot", ManifestRevision: first}},
		once:             &sync.Once{}, ready: ready,
	}
	registry := NewRegistry()
	var dispatched atomic.Int32
	if err := registry.Register(VerbSSHObserve, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
		dispatched.Add(1)
		return struct{}{}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(registry, memoryJournal{}, initial, "boot")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "authority.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener, RolePanel) }()

	oldConnection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer oldConnection.Close()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("old authority request was not admitted")
	}
	next := revisionAttestor{revision: second, peer: PeerIdentity{BootID: "boot", ManifestRevision: second}}
	rotateContext, rotateCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rotateCancel()
	if err := server.RotateAuthority(rotateContext, next, second); err != nil {
		t.Fatal(err)
	}
	_ = oldConnection.SetDeadline(time.Now().Add(time.Second))
	oldRequest := brokerReadRequest(t, time.Now(), VerbSSHObserve)
	if err := WriteFrame(oldConnection, oldRequest, MaxRequestBytes); err == nil {
		var oldResponse Response
		if readErr := ReadFrame(oldConnection, &oldResponse, MaxResponseBytes); readErr == nil && oldResponse.OK {
			t.Fatal("old open connection dispatched after authority rotation")
		}
	}
	if dispatched.Load() != 0 {
		t.Fatalf("old generation dispatched %d handlers", dispatched.Load())
	}

	newConnection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	request := brokerReadRequest(t, time.Now(), VerbSSHObserve)
	if err := WriteFrame(newConnection, request, MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := ReadFrame(newConnection, &response, MaxResponseBytes); err != nil {
		t.Fatal(err)
	}
	_ = newConnection.Close()
	if !response.OK || dispatched.Load() != 1 || server.AuthorityRevision() != second {
		t.Fatalf("new response=%+v dispatched=%d revision=%q", response, dispatched.Load(), server.AuthorityRevision())
	}
	cancel()
	_ = listener.Close()
	server.ShutdownConnections()
	server.WaitConnections()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
