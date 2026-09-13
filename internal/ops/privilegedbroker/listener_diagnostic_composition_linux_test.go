//go:build linux

package privilegedbroker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
)

func TestListenerFailureStagesCrossProductionBrokerWithOneSanitizedPublicFailure(t *testing.T) {
	for _, test := range []struct {
		stage  string
		reason listenerevidence.Reason
	}{
		{stage: "pidfd_open", reason: listenerevidence.ReasonPIDFDAuthority},
		{stage: "process_generation_before", reason: listenerevidence.ReasonProcessChanged},
		{stage: "descriptor_inventory", reason: listenerevidence.ReasonDescriptorInventory},
		{stage: "socket_table_open", reason: listenerevidence.ReasonSocketTable},
		{stage: "socket_table_read", reason: listenerevidence.ReasonSocketTable},
		{stage: "socket_table_parse", reason: listenerevidence.ReasonSocketAmbiguous},
		{stage: "process_generation_after", reason: listenerevidence.ReasonProcessChanged},
		{stage: "stability_fence", reason: listenerevidence.ReasonObservationChanged},
	} {
		t.Run(test.stage, func(t *testing.T) {
			uid, supported := diagnosticCurrentUID()
			if !supported {
				t.Skip("diagnostic owner/mode contract requires Linux")
			}
			ring, err := openRecentDiagnosticRingAt(diagnosticFixtureRoot(t), uid)
			if err != nil {
				t.Fatal(err)
			}
			failure := &listenerevidence.Error{Reason: test.reason, Diagnostic: listenerevidence.Diagnostic{
				ProofMethod: hostfacts.ListenerProofProcFSV1, Stage: test.stage, ErrnoClass: "EIO",
				DescriptorCount: 4, SocketDescriptorCount: 2, DuplicateAttempts: 1, RetryCount: 1,
			}}
			registry := NewRegistry()
			if err := registry.Register(VerbSSHObserve, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
				return nil, failure
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
				t.Fatalf("public failure changed for %s: %#v", test.stage, response)
			}
			document, err := readRecentDiagnosticsAt(filepath.Join(ring.root, "recent.json"), uid)
			if err != nil || len(document.Records) != 1 {
				t.Fatalf("recent diagnostic document = %#v, %v", document, err)
			}
			diagnostic := document.Records[0]
			if diagnostic.Operation != VerbSSHObserve || diagnostic.Owner != "listener_evidence" ||
				diagnostic.Reason != string(test.reason) || diagnostic.Stage != test.stage ||
				diagnostic.ErrnoClass != "EIO" || diagnostic.ProofMethod != hostfacts.ListenerProofProcFSV1 ||
				diagnostic.DescriptorCount != 4 || diagnostic.SocketDescriptorCount != 2 ||
				diagnostic.DuplicateAttempts != 1 || diagnostic.RetryCount != 1 {
				t.Fatalf("bounded listener diagnostic was not preserved: %#v", diagnostic)
			}
		})
	}
}
