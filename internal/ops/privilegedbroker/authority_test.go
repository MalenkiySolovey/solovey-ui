package privilegedbroker

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

type revisionAttestor struct {
	revision string
	peer     PeerIdentity
}

func (a revisionAttestor) Attest(context.Context, *net.UnixConn, Role) (PeerIdentity, error) {
	return a.peer, nil
}
func (revisionAttestor) Recheck(context.Context, PeerIdentity, Role) error { return nil }
func (a revisionAttestor) AuthorityRevision() string                       { return a.revision }

func TestAuthorityRotationFencesOldGenerationAndSupportsRollback(t *testing.T) {
	first := Digest([]byte("authority-first"))
	second := Digest([]byte("authority-second"))
	server, err := NewServer(NewRegistry(), memoryJournal{}, revisionAttestor{revision: first}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := server.beginAuthority(context.Background())
	if err != nil || lease.Revision != first {
		t.Fatalf("initial lease=%+v err=%v", lease, err)
	}
	rotated := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		rotated <- server.RotateAuthority(ctx, revisionAttestor{revision: second}, second)
	}()
	select {
	case <-lease.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("retiring generation did not cancel its active lease")
	}
	if _, err := server.beginAuthority(context.Background()); err == nil || peerAttestationClass(err) != PeerAttestationGenerationMismatch {
		t.Fatalf("request admitted during retirement: %v", err)
	}
	lease.Release()
	if err := <-rotated; err != nil {
		t.Fatal(err)
	}
	if server.AuthorityRevision() != second || server.authorityCurrent(first) {
		t.Fatal("new authority generation was not sealed")
	}
	newLease, err := server.beginAuthority(context.Background())
	if err != nil || newLease.Revision != second {
		t.Fatalf("new lease=%+v err=%v", newLease, err)
	}
	newLease.Release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.RotateAuthority(ctx, revisionAttestor{revision: first}, first); err != nil || server.AuthorityRevision() != first {
		t.Fatalf("authority rollback failed: %v", err)
	}
}

func TestAuthorityRotationTimeoutLeavesFailClosedRetiringFence(t *testing.T) {
	first := Digest([]byte("authority-timeout-first"))
	second := Digest([]byte("authority-timeout-second"))
	server, err := NewServer(NewRegistry(), memoryJournal{}, revisionAttestor{revision: first}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := server.beginAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := server.RotateAuthority(ctx, revisionAttestor{revision: second}, second); err == nil {
		t.Fatal("undrained authority generation unexpectedly rotated")
	}
	if _, err := server.beginAuthority(context.Background()); err == nil || !errors.As(err, new(*peerAttestationError)) {
		t.Fatalf("retiring fence reopened after timeout: %v", err)
	}
	lease.Release()
}

func TestAuthorityRotationRejectsRevisionSubstitution(t *testing.T) {
	first := Digest([]byte("authority-substitution-first"))
	second := Digest([]byte("authority-substitution-second"))
	server, err := NewServer(NewRegistry(), memoryJournal{}, revisionAttestor{revision: first}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.RotateAuthority(context.Background(), revisionAttestor{revision: first}, second); err == nil {
		t.Fatal("attestor revision substitution unexpectedly accepted")
	}
}
