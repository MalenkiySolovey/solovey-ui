package privilegedbroker

import (
	"context"
	"testing"
	"time"
)

type boundedHandlerFailure struct{}

func (boundedHandlerFailure) Error() string { return "secret raw host failure" }

func (boundedHandlerFailure) BrokerDiagnostic() (string, string, string, string, string) {
	return "listener_evidence", "socket_table_unavailable", "socket_table_open", "EACCES", "procfs_fd_inode_socket_table/v1"
}

func (boundedHandlerFailure) BrokerDiagnosticCounters() (int, int, int, int, int) {
	return 17, 3, 2, 0, 1
}

func TestHandlerDiagnosticPreservesBoundedInternalBranchAndSanitizesPublicError(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(VerbSSHObserve, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
		return nil, boundedHandlerFailure{}
	}}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(registry, memoryJournal{}, StaticAttestor{Peer: PeerIdentity{Revision: Digest([]byte("peer"))}}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0).UTC()
	server.Now = func() time.Time { return now }
	var diagnostic DiagnosticEvent
	server.Diagnostic = func(event DiagnosticEvent) { diagnostic = event }
	response := server.Handle(context.Background(), brokerReadRequest(t, now, VerbSSHObserve), PeerIdentity{Revision: Digest([]byte("peer"))})
	if response.Code != CodeInternal || response.Message != "broker operation failed" {
		t.Fatalf("public failure was not sanitized: %#v", response)
	}
	if diagnostic.Operation != VerbSSHObserve || diagnostic.HandlerOwner != "listener_evidence" || diagnostic.HandlerReason != "socket_table_unavailable" ||
		diagnostic.HandlerStage != "socket_table_open" || diagnostic.HandlerErrno != "EACCES" ||
		diagnostic.ProofMethod != "procfs_fd_inode_socket_table/v1" || diagnostic.DescriptorCount != 17 ||
		diagnostic.SocketDescriptorCount != 3 || diagnostic.DuplicateAttempts != 2 || diagnostic.DuplicatedSockets != 0 ||
		diagnostic.RetryCount != 1 {
		t.Fatalf("bounded handler diagnostic was lost: %#v", diagnostic)
	}
}
