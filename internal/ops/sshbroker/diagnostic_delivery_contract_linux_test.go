//go:build linux && solovey_contract

package sshbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

type diagnosticContractJournal struct{}

type diagnosticContractAttestor struct {
	peer broker.PeerIdentity
}

func (a diagnosticContractAttestor) Attest(context.Context, *net.UnixConn, broker.Role) (broker.PeerIdentity, error) {
	return a.peer, nil
}
func (diagnosticContractAttestor) Recheck(context.Context, broker.PeerIdentity, broker.Role) error {
	return nil
}

func (diagnosticContractJournal) Begin(broker.Request, broker.PeerIdentity, string, time.Time) (*broker.Response, *broker.Receipt, error) {
	return nil, nil, nil
}
func (diagnosticContractJournal) Commit(_ broker.Request, _ *broker.Receipt, response broker.Response, _ broker.CompletionPolicy, _ time.Time) (broker.Response, error) {
	return response, nil
}
func (diagnosticContractJournal) Unresolved() []broker.Receipt { return nil }

func TestRealSSHObserveHandlerPersistsAndRereadsEveryListenerFailureFamily(t *testing.T) {
	tests := []struct {
		name, owner, stage, reason, errno, proof string
		failure                                  error
		listenerCounters                         bool
		panicOwner                               bool
		publicMessage                            string
	}{
		{name: "process fence", owner: "listener_evidence", stage: "pidfd_open", reason: string(listenerevidence.ReasonPIDFDAuthority), errno: "EPERM", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerContractFailure(listenerevidence.ReasonPIDFDAuthority, "pidfd_open", "EPERM"), listenerCounters: true},
		{name: "process generation before", owner: "listener_evidence", stage: "process_generation_before", reason: string(listenerevidence.ReasonProcessChanged), errno: "EIO", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerContractFailure(listenerevidence.ReasonProcessChanged, "process_generation_before", "EIO"), listenerCounters: true},
		{name: "fd snapshot", owner: "listener_evidence", stage: "descriptor_inventory", reason: string(listenerevidence.ReasonDescriptorInventory), errno: "EACCES", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerContractFailure(listenerevidence.ReasonDescriptorInventory, "descriptor_inventory", "EACCES"), listenerCounters: true},
		{name: "socket table open", owner: "listener_evidence", stage: "socket_table_open", reason: string(listenerevidence.ReasonSocketTable), errno: "EACCES", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerContractFailure(listenerevidence.ReasonSocketTable, "socket_table_open", "EACCES"), listenerCounters: true},
		{name: "socket table read", owner: "listener_evidence", stage: "socket_table_read", reason: string(listenerevidence.ReasonSocketTable), errno: "EIO", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerContractFailure(listenerevidence.ReasonSocketTable, "socket_table_read", "EIO"), listenerCounters: true},
		{name: "socket table parse", owner: "listener_evidence", stage: "socket_table_parse", reason: string(listenerevidence.ReasonSocketAmbiguous), proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerContractFailure(listenerevidence.ReasonSocketAmbiguous, "socket_table_parse", ""), listenerCounters: true},
		{name: "process generation after", owner: "listener_evidence", stage: "process_generation_after", reason: string(listenerevidence.ReasonProcessChanged), errno: "ESRCH", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerContractFailure(listenerevidence.ReasonProcessChanged, "process_generation_after", "ESRCH"), listenerCounters: true},
		{name: "stability recheck", owner: "listener_evidence", stage: "stability_fence", reason: string(listenerevidence.ReasonObservationChanged), proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerContractFailure(listenerevidence.ReasonObservationChanged, "stability_fence", ""), listenerCounters: true},
		{name: "configuration read", owner: "ssh_listener_authority", stage: "configuration_read", reason: "configuration_evidence_unavailable",
			failure: listenerAuthorityWrap(errors.New("private UCI failure"), "configuration_evidence_unavailable", "configuration_read", "", "")},
		{name: "service observation", owner: "ssh_listener_authority", stage: "service_observe", reason: "service_evidence_unavailable",
			failure: listenerAuthorityWrap(errors.New("private procd failure"), "service_evidence_unavailable", "service_observe", "", "")},
		{name: "process identity", owner: "ssh_listener_authority", stage: "process_fence", reason: "process_identity_unavailable",
			failure: listenerAuthorityWrap(errors.New("private process failure"), "process_identity_unavailable", "process_fence", "", "")},
		{name: "semantic configured match", owner: "ssh_listener_authority", stage: "configured_listener_match", reason: "socket_not_accepting", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerAuthorityFailure("socket_not_accepting", "configured_listener_match", "", hostfacts.ListenerProofProcFSV1)},
		{name: "semantic command match", owner: "ssh_listener_authority", stage: "procd_command_match", reason: "socket_identity_mismatch", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerAuthorityFailure("socket_identity_mismatch", "procd_command_match", "", hostfacts.ListenerProofProcFSV1)},
		{name: "semantic inode correlation", owner: "ssh_listener_authority", stage: "inode_correlate", reason: "socket_identity_invalid", proof: hostfacts.ListenerProofProcFSV1,
			failure: listenerAuthorityWrap(errors.New("private projection failure"), "socket_identity_invalid", "inode_correlate", "", hostfacts.ListenerProofProcFSV1)},
		{name: "semantic stability recheck", owner: "ssh_listener_authority", stage: "stability_recheck", reason: "process_generation_changed",
			failure: listenerAuthorityWrap(errors.New("private recheck failure"), "process_generation_changed", "stability_recheck", "", "")},
		{name: "semantic authority validation", owner: "ssh_listener_authority", stage: "authority_validate", reason: "runtime_posture_invalid",
			failure: listenerAuthorityWrap(errors.New("private posture failure"), "runtime_posture_invalid", "authority_validate", "", "")},
		{name: "generic natural branch", owner: "ssh_listener_authority", stage: "authority_validate", reason: "observation_failed",
			failure: errors.New("private natural owner error")},
		{name: "unexpected owner panic", owner: "ssh_listener_authority", stage: "authority_validate", reason: "unexpected_failure", panicOwner: true, publicMessage: "broker handler failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "diagnostics")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			uid := uint32(os.Geteuid())
			ring, err := broker.OpenRecentDiagnosticRingAtForContract(root, uid)
			if err != nil {
				t.Fatal(err)
			}
			// This is the production SSH handler. Injection occurs at its private
			// owner-local observation seam, before the repaired classification and
			// before broker public sanitization.
			host := &Host{implementation: ImplementationDropbear, observePosture: func(context.Context) (ObservationV1, error) {
				if test.panicOwner {
					panic("private owner panic")
				}
				return ObservationV1{}, fmt.Errorf("owner adapter context: %w", test.failure)
			}}
			registry := broker.NewRegistry()
			if err := registerHostHandlers(registry, host); err != nil {
				t.Fatal(err)
			}
			server, err := broker.NewServer(registry, diagnosticContractJournal{}, diagnosticContractAttestor{peer: broker.PeerIdentity{Revision: broker.Digest([]byte("peer"))}}, "boot")
			if err != nil {
				t.Fatal(err)
			}
			now := time.Unix(1_900_000_000, 0).UTC()
			server.Now = func() time.Time { return now }
			var recordErr error
			server.Diagnostic = func(event broker.DiagnosticEvent) { recordErr = ring.Record(event) }
			response := server.Handle(context.Background(), diagnosticContractRequest(t, now), broker.PeerIdentity{Revision: broker.Digest([]byte("peer"))})
			if recordErr != nil {
				t.Fatal(recordErr)
			}
			expectedPublicMessage := test.publicMessage
			if expectedPublicMessage == "" {
				expectedPublicMessage = "broker operation failed"
			}
			if response.Code != broker.CodeInternal || response.Message != expectedPublicMessage {
				t.Fatalf("public failure changed: %#v", response)
			}
			document, err := broker.ReadRecentDiagnosticsAtForContract(filepath.Join(root, "recent.json"), uid)
			if err != nil || len(document.Records) != 1 {
				t.Fatalf("operator-compatible reread = %#v, %v", document, err)
			}
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			var reread broker.RecentDiagnostics
			if err := json.Unmarshal(encoded, &reread); err != nil || reread.Schema != broker.RecentDiagnosticSchema || len(reread.Records) != 1 {
				t.Fatalf("serialized operator document = %#v, %v", reread, err)
			}
			diagnostic := reread.Records[0]
			if diagnostic.Operation != broker.VerbSSHObserve || diagnostic.Owner != test.owner || diagnostic.Stage != test.stage ||
				diagnostic.Reason != test.reason || diagnostic.ErrnoClass != test.errno || diagnostic.ProofMethod != test.proof {
				t.Fatalf("persisted diagnostic = %#v", diagnostic)
			}
			if test.listenerCounters && (diagnostic.DescriptorCount != 4 || diagnostic.SocketDescriptorCount != 2 ||
				diagnostic.DuplicateAttempts != 1 || diagnostic.RetryCount != 1) {
				t.Fatalf("listener counters were lost: %#v", diagnostic)
			}
		})
	}
}

func listenerContractFailure(reason listenerevidence.Reason, stage, errno string) error {
	failure := &listenerevidence.Error{Reason: reason, Diagnostic: listenerevidence.Diagnostic{
		Stage: stage, ErrnoClass: errno, ProofMethod: hostfacts.ListenerProofProcFSV1,
		DescriptorCount: 4, SocketDescriptorCount: 2, DuplicateAttempts: 1, RetryCount: 1,
	}}
	return fmt.Errorf("adapter: %w", fmt.Errorf("observer: %w", failure))
}

func diagnosticContractRequest(t *testing.T, now time.Time) broker.Request {
	t.Helper()
	payload, digest, err := broker.MarshalPayload(EmptyV1{})
	if err != nil {
		t.Fatal(err)
	}
	return broker.Request{ProtocolVersion: broker.ProtocolVersion, CapabilityRevision: broker.CapabilityRevision,
		BootID: "boot", Role: broker.RolePanel, Verb: broker.VerbSSHObserve, RequestID: "request-1", OperationID: "operation-1",
		DeadlineAt: now.Add(time.Minute).UnixMilli(), Payload: payload, PayloadDigest: digest}
}
