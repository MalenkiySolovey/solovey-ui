//go:build linux

package update

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// Use the real wire, dispatcher and durable journal. A transport stub that
// always acknowledges requests cannot detect fences leaking across operations.
func TestUpdateMutationFencesFollowDurableOperationAcrossBrokerRestart(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the actual broker journal requires a Root-owned fixture")
	}
	root := filepath.Join(t.TempDir(), "solovey-ui-broker")
	registry := broker.NewRegistry()
	calls := 0
	if err := registry.Register(broker.VerbUpdateStage, broker.Definition{Role: broker.RolePanel, Mutation: true,
		Handler: func(context.Context, broker.Request, broker.PeerIdentity) (any, error) {
			calls++
			return nil, nil
		}}); err != nil {
		t.Fatal(err)
	}
	const boot = "update-fence-host"
	open := func() *broker.Server {
		journal, err := broker.OpenFileJournal(root, boot)
		if err != nil {
			t.Fatal(err)
		}
		return &broker.Server{Registry: registry, Journal: journal, BootID: boot, Now: time.Now}
	}
	server := open()
	previous := model.UpdateOperation{OperationID: "update-operation:previous-provider", Revision: 12, Sequence: 2,
		ManifestDigest: lifecycleDigest("same-envelope"), DeploymentRevision: lifecycleDigest("same-deployment")}
	body := struct{ Part string }{"first"}
	request, _, err := sendUpdateFenceMutation(t, server, previous, 900_001, body)
	if err != nil {
		t.Fatal("previous operation failed", err)
	}
	// Existing global fence authority must remain intact across the client fix.
	legacy := request
	legacy.OperationID, legacy.IdempotencyKey = "update-operation:legacy-global", "update-legacy-global"
	legacy.Fence.Resource, legacy.Fence.Sequence = "release-activation", 99_000_001
	if response := server.Handle(t.Context(), legacy, broker.PeerIdentity{}); !response.OK {
		t.Fatal("legacy global fence fixture failed", response.Code)
	}
	server = open()
	next := previous
	next.OperationID, next.Revision = "update-operation:return-provider", 1
	nextRequest, response, err := sendUpdateFenceMutation(t, server, next, 1, body)
	if err != nil || !response.OK {
		t.Fatal("a different durable operation inherited the previous fence", response.Code, err)
	}
	server = open()
	response = server.Handle(t.Context(), nextRequest, broker.PeerIdentity{Revision: lifecycleDigest("journal-peer")})
	if !response.OK || !response.Replay || calls != 3 {
		t.Fatal("exact wire retry lost durable replay or repeated its handler", response.Code, calls)
	}
	_, response, err = sendUpdateFenceMutation(t, server, next, 1, struct{ Part string }{"changed"})
	if !errors.Is(err, ErrRevisionMismatch) || response.Code != broker.CodeFence || calls != 3 {
		t.Fatal("a stale mutation within one operation passed its fence", response.Code, err, calls)
	}
}

func sendUpdateFenceMutation(t *testing.T, server *broker.Server, operation model.UpdateOperation, ordinal uint64, payload any) (broker.Request, broker.Response, error) {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type result struct {
		request  broker.Request
		response broker.Response
		err      error
	}
	done := make(chan result, 1)
	go func() {
		var received result
		received.err = broker.ReadFrame(serverSide, &received.request, broker.MaxRequestBytes)
		if received.err == nil {
			received.response = server.Handle(ctx, received.request, broker.PeerIdentity{Revision: lifecycleDigest("journal-peer")})
			received.err = broker.WriteFrame(serverSide, received.response, broker.MaxResponseBytes)
		}
		done <- received
	}()
	provider := &BrokerProvider{Client: &broker.Client{Role: broker.RolePanel, BootID: server.BootID,
		Dial: func(context.Context, string) (net.Conn, error) { return clientSide, nil }}}
	err := provider.mutate(ctx, broker.VerbUpdateStage, operation, ordinal, payload, nil)
	select {
	case received := <-done:
		if received.err != nil {
			t.Fatal("wire fixture failed", received.err)
		}
		return received.request, received.response, err
	case <-ctx.Done():
		t.Fatal("wire fixture exceeded its bound")
		return broker.Request{}, broker.Response{}, ctx.Err()
	}
}
