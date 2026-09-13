package privilegedbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func BenchmarkBrokerEncodeDecode(b *testing.B) {
	now := time.Unix(1_900_000_000, 0).UTC()
	request := benchmarkMutationRequest(now)
	var frame bytes.Buffer
	if err := WriteFrame(&frame, request, MaxRequestBytes); err != nil {
		b.Fatal(err)
	}
	data := append([]byte(nil), frame.Bytes()...)
	b.ReportAllocs()
	for b.Loop() {
		var decoded Request
		if err := ReadFrame(bytes.NewReader(data), &decoded, MaxRequestBytes); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPeerManifestAttestationProjection(b *testing.B) {
	digest := Digest([]byte("executable"))
	manifest := Manifest{Clients: []ClientManifest{{Name: "panel", UID: 1001, GID: 1001, Executable: "/usr/local/solovey-ui/solovey-ui",
		ExecutableDigest: digest, Device: 7, Inode: 11, CgroupUnit: "solovey-ui.service", Roles: []Role{RolePanel}}}}
	peer := PeerIdentity{UID: 1001, GID: 1001, Executable: "/usr/local/solovey-ui/solovey-ui", ExecutableDigest: digest,
		Device: 7, Inode: 11, CgroupUnit: "solovey-ui.service"}
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := manifest.matching(RolePanel, peer); !ok {
			b.Fatal("peer rejected")
		}
	}
}

func BenchmarkBrokerVerbDispatch(b *testing.B) {
	registry := NewRegistry()
	verb := Verb("deployment.benchmark.observe")
	if err := registry.Register(verb, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
		return struct{}{}, nil
	}}); err != nil {
		b.Fatal(err)
	}
	server, err := NewServer(registry, memoryJournal{}, StaticAttestor{Peer: PeerIdentity{Revision: Digest([]byte("peer"))}}, "boot")
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	server.Now = func() time.Time { return now }
	request := benchmarkReadRequest(now, verb)
	b.ReportAllocs()
	for b.Loop() {
		if response := server.Handle(context.Background(), request, PeerIdentity{}); !response.OK {
			b.Fatal(response.Code)
		}
	}
}

func BenchmarkBrokerJournalReplayLookup(b *testing.B) {
	now := time.Unix(1_900_000_000, 0).UTC()
	request := benchmarkMutationRequest(now)
	digest := Digest(append(canonicalRequestAuthority(request), request.Payload...))
	journal := &FileJournal{replays: map[string]replayRecord{"benchmark": {requestDigest: digest, response: successResponse(request)}},
		fences: make(map[string]uint64), unresolved: make(map[string]Receipt)}
	b.ReportAllocs()
	for b.Loop() {
		response, _, err := journal.Begin(request, PeerIdentity{}, digest, now)
		if err != nil || response == nil || !response.Replay {
			b.Fatal("replay lookup failed")
		}
	}
}

func benchmarkMutationRequest(now time.Time) Request {
	payload, payloadDigest, _ := MarshalPayload(protocolFixture{Value: "ok"})
	return Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, BootID: "boot", Role: RolePanel,
		Verb: "deployment.benchmark.apply", RequestID: "request-benchmark", OperationID: "operation-benchmark", IdempotencyKey: "benchmark",
		Fence: Fence{Resource: "deployment", Sequence: 1, Token: Digest([]byte("token"))}, DeadlineAt: now.Add(time.Minute).UnixMilli(),
		Payload: payload, PayloadDigest: payloadDigest}
}

func benchmarkReadRequest(now time.Time, verb Verb) Request {
	payload, payloadDigest, _ := MarshalPayload(struct{}{})
	return Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, BootID: "boot", Role: RolePanel,
		Verb: verb, RequestID: "request-benchmark", OperationID: "operation-benchmark", DeadlineAt: now.Add(time.Minute).UnixMilli(),
		Payload: payload, PayloadDigest: payloadDigest}
}

func TestJournalRetentionProjectionIsBoundedAndKeepsNewestAuthority(t *testing.T) {
	rows := make([]journalRow, 0, maxJournalRows+17)
	for index := 1; index <= maxJournalRows+17; index++ {
		rows = append(rows, testCompletedJournalRow(uint64(index), fmt.Sprintf("retention-operation-%04d", index), fmt.Sprintf("retention-idempotency-%04d", index),
			Verb("deployment.test.apply"), "deployment", uint64(index), json.RawMessage(`{"value":"ok"}`), false, ""))
	}
	kept, _, _, err := retainedJournalProjection(rows, -1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) > targetJournalRows || kept[0].Phase != "authority" || kept[0].Authority.Fences["deployment"] != maxJournalRows+17 ||
		kept[len(kept)-1].Receipt.FenceSequence != uint64(maxJournalRows+17) {
		t.Fatalf("retention projection=%d state=%#v last=%#v", len(kept), kept[0], kept[len(kept)-1])
	}
}
