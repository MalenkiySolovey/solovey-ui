package main

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

type watcherAttestor struct{ revision string }

func (watcherAttestor) Attest(context.Context, *net.UnixConn, broker.Role) (broker.PeerIdentity, error) {
	return broker.PeerIdentity{}, nil
}
func (watcherAttestor) Recheck(context.Context, broker.PeerIdentity, broker.Role) error { return nil }
func (a watcherAttestor) AuthorityRevision() string                                     { return a.revision }

func TestAuthorityWatcherRotatesAndRollsBackValidatedManifestGeneration(t *testing.T) {
	first := broker.Digest([]byte("watcher-first"))
	second := broker.Digest([]byte("watcher-second"))
	server, err := broker.NewServer(broker.NewRegistry(), startupJournal{}, watcherAttestor{revision: first}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	var lock sync.Mutex
	desired := broker.Manifest{Schema: broker.ManifestSchemaSystemd, Revision: second}
	load := func() (broker.Manifest, error) {
		lock.Lock()
		defer lock.Unlock()
		return desired, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- watchAuthorityManifestWith(ctx, server, first, 5*time.Millisecond, load) }()
	waitAuthorityRevision(t, server, second)
	lock.Lock()
	desired = broker.Manifest{Schema: broker.ManifestSchemaSystemd, Revision: first}
	lock.Unlock()
	waitAuthorityRevision(t, server, first)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func waitAuthorityRevision(t *testing.T, server *broker.Server, expected string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for server.AuthorityRevision() != expected && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if actual := server.AuthorityRevision(); actual != expected {
		t.Fatalf("authority revision=%q want=%q", actual, expected)
	}
}

func TestAuthorityWatcherKeepsRotatingDuringPendingMaintenanceRecovery(t *testing.T) {
	first, second := broker.Digest([]byte("maintenance-first")), broker.Digest([]byte("maintenance-second"))
	server, err := broker.NewServer(broker.NewRegistry(), startupJournal{}, watcherAttestor{revision: first}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- watchAuthorityManifestWithMaintenance(ctx, server, first, time.Millisecond, func() (broker.Manifest, error) {
			return broker.Manifest{Schema: broker.ManifestSchemaSystemd, Revision: second}, nil
		}, func(ctx context.Context) error {
			if _, bounded := ctx.Deadline(); !bounded {
				t.Error("maintenance callback has no execution deadline")
				return nil
			}
			return errors.New("private recovery error must not be logged")
		})
	}()
	waitAuthorityRevision(t, server, second)
	cancel()
	if err := <-done; err != nil {
		t.Fatal("pending maintenance stopped the broker's authority watcher", err)
	}
}

func TestAuthorityWatcherCancellationJoinsMaintenanceCallback(t *testing.T) {
	first := broker.Digest([]byte("maintenance-cancel"))
	server, err := broker.NewServer(broker.NewRegistry(), startupJournal{}, watcherAttestor{revision: first}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- watchAuthorityManifestWithMaintenance(ctx, server, first, time.Millisecond, func() (broker.Manifest, error) {
			return broker.Manifest{Schema: broker.ManifestSchemaSystemd, Revision: first}, nil
		}, func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("authority watcher outlived cancellation of its maintenance callback")
	}
}
