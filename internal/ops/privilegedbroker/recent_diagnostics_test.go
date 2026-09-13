package privilegedbroker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProductionBrokerDispatchPublishesTypedRecentDiagnosticAndKeepsPublicFailureSanitized(t *testing.T) {
	uid, supported := diagnosticCurrentUID()
	if !supported {
		t.Skip("diagnostic owner/mode contract requires Linux")
	}
	ring, err := openRecentDiagnosticRingAt(diagnosticFixtureRoot(t), uid)
	if err != nil {
		t.Fatal(err)
	}
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
	var recordErr error
	server.Diagnostic = func(event DiagnosticEvent) { recordErr = ring.Record(event) }
	response := server.Handle(context.Background(), brokerReadRequest(t, now, VerbSSHObserve), PeerIdentity{Revision: Digest([]byte("peer"))})
	if recordErr != nil {
		t.Fatal(recordErr)
	}
	if response.Code != CodeInternal || response.Message != "broker operation failed" {
		t.Fatalf("public response = %#v", response)
	}
	if len(ring.records) != 1 || ring.records[0].Operation != VerbSSHObserve ||
		ring.records[0].Stage != "socket_table_open" || ring.records[0].Reason != "socket_table_unavailable" {
		t.Fatalf("recent diagnostic = %#v", ring.records)
	}
}

func TestDefaultRecentDiagnosticReadRequiresRoot(t *testing.T) {
	uid, supported := diagnosticCurrentUID()
	if !supported {
		t.Skip("diagnostic owner/mode contract requires Linux")
	}
	if uid == 0 {
		t.Skip("non-root process is required for the denial half of the root-only contract")
	}
	if _, err := ReadRecentDiagnostics(); err == nil {
		t.Fatal("non-root process read the product-owned recent diagnostic path")
	}
	if _, err := OpenRecentDiagnosticRing(); err == nil {
		t.Fatal("non-root process opened the product-owned recent diagnostic writer")
	}
}

func TestRecentDiagnosticRingIsAlwaysReadableBoundedAndOwnerClassified(t *testing.T) {
	uid, supported := diagnosticCurrentUID()
	if !supported {
		t.Skip("diagnostic owner/mode contract requires Linux")
	}
	root := diagnosticFixtureRoot(t)
	ring, err := openRecentDiagnosticRingAt(root, uid)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < MaxRecentDiagnosticRecords+6; index++ {
		event := DiagnosticEvent{
			Timestamp: time.UnixMilli(1_900_000_000_000 + int64(index)), Operation: VerbSSHObserve,
			HandlerOwner: "listener_evidence", HandlerStage: "socket_table_read",
			HandlerReason: "socket_table_unavailable", HandlerErrno: "EACCES",
			ProofMethod: "procfs_fd_inode_socket_table/v1", DescriptorCount: index,
			SocketDescriptorCount: 2, DuplicateAttempts: 1, RetryCount: 1,
		}
		if err := ring.Record(event); err != nil {
			t.Fatal(err)
		}
	}
	document, err := readRecentDiagnosticsAt(filepath.Join(root, "recent.json"), uid)
	if err != nil {
		t.Fatal(err)
	}
	if document.Schema != RecentDiagnosticSchema || len(document.Records) != MaxRecentDiagnosticRecords {
		t.Fatalf("recent diagnostic envelope = %#v", document)
	}
	first, last := document.Records[0], document.Records[len(document.Records)-1]
	if first.DescriptorCount != 6 || last.DescriptorCount != MaxRecentDiagnosticRecords+5 ||
		last.Owner != "listener_evidence" || last.Operation != VerbSSHObserve ||
		last.Stage != "socket_table_read" || last.Reason != "socket_table_unavailable" ||
		last.ErrnoClass != "EACCES" || last.ProofMethod != "procfs_fd_inode_socket_table/v1" {
		t.Fatalf("bounded recent projection = first=%#v last=%#v", first, last)
	}
	info, err := os.Lstat(filepath.Join(root, "recent.json"))
	if err != nil || info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
		t.Fatalf("recent diagnostic file = %v, %v", info, err)
	}
}

func TestRecentDiagnosticRingIgnoresUntypedDenialsAndRejectsUnsafeFields(t *testing.T) {
	uid, supported := diagnosticCurrentUID()
	if !supported {
		t.Skip("diagnostic owner/mode contract requires Linux")
	}
	root := diagnosticFixtureRoot(t)
	ring, err := openRecentDiagnosticRingAt(root, uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Record(DiagnosticEvent{Timestamp: time.Now().UTC(), Operation: VerbSSHObserve, ResultClass: "denied_internal_error"}); err != nil {
		t.Fatal(err)
	}
	if len(ring.records) != 0 {
		t.Fatal("untyped broker denial entered the owner-classified ring")
	}
	unsafe := DiagnosticEvent{Timestamp: time.Now().UTC(), Operation: VerbSSHObserve,
		HandlerOwner: "listener_evidence", HandlerStage: "/proc/123/fd", HandlerReason: "raw\nsecret"}
	if err := ring.Record(unsafe); err == nil {
		t.Fatal("unsafe diagnostic fields were accepted")
	}
}

func TestRecentDiagnosticRingRejectsUntrustedPathAndDocument(t *testing.T) {
	uid, supported := diagnosticCurrentUID()
	if !supported {
		t.Skip("diagnostic owner/mode contract requires Linux")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "diagnostics")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := openRecentDiagnosticRingAt(root, uid); err == nil {
		t.Fatal("group/world-readable diagnostic root was accepted")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	malformed := RecentDiagnostics{Schema: RecentDiagnosticSchema, Records: []RecentDiagnostic{{
		Timestamp: time.Now().UnixMilli(), Owner: "listener_evidence", Operation: VerbSSHObserve,
		Stage: "socket_table_read", Reason: strings.Repeat("x", 97),
	}}}
	data, err := json.Marshal(malformed)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "recent.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openRecentDiagnosticRingAt(root, uid); err == nil {
		t.Fatal("malformed diagnostic document was accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if _, err := openRecentDiagnosticRingAt(root, uid); err == nil {
		t.Fatal("symlinked diagnostic root was accepted")
	}
}

func diagnosticFixtureRoot(t testing.TB) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "diagnostics")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}
