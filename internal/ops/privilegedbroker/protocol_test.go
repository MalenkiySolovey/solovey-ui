package privilegedbroker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type protocolFixture struct {
	Value string `json:"value"`
}

type memoryJournal struct{}

func (memoryJournal) Begin(Request, PeerIdentity, string, time.Time) (*Response, *Receipt, error) {
	return nil, nil, nil
}
func (memoryJournal) Commit(_ Request, receipt *Receipt, response Response, _ time.Time) (Response, error) {
	response.Receipt = receipt
	return response, nil
}
func (memoryJournal) Unresolved() []Receipt { return nil }

func TestStrictProtocolRejectsMalformedAndUnboundedFrames(t *testing.T) {
	for name, data := range map[string][]byte{
		"unknown":   []byte(`{"value":"ok","command":"id"}`),
		"duplicate": []byte(`{"value":"one","value":"two"}`),
		"multiple":  []byte(`{"value":"one"}{"value":"two"}`),
		"truncated": append([]byte{0, 0, 0, 20}, []byte(`{"value":"x"}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			var target protocolFixture
			var err error
			if name == "truncated" {
				err = ReadFrame(bytes.NewReader(data), &target, 1024)
			} else {
				err = DecodePayload(data, &target)
			}
			if err == nil {
				t.Fatalf("unsafe payload accepted: %s", data)
			}
		})
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 1025)
	if err := ReadFrame(bytes.NewReader(header[:]), &protocolFixture{}, 1024); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if err := WriteFrame(&bytes.Buffer{}, strings.Repeat("x", 1025), 1024); err == nil {
		t.Fatal("oversized response frame accepted")
	}
	deep := []byte(strings.Repeat("[", MaxJSONDepth+2) + "0" + strings.Repeat("]", MaxJSONDepth+2))
	var target any
	if err := DecodePayload(deep, &target); err == nil {
		t.Fatal("excessively nested JSON accepted")
	}
}

func TestRequestAuthorityValidationIsFailClosed(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	definition := Definition{Role: RolePanel, Mutation: true, Handler: func(context.Context, Request, PeerIdentity) (any, error) { return nil, nil }}
	valid := brokerMutationRequest(t, now, 1, "idem-one")
	if err := valid.Validate(now, definition); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Request){
		"version":  func(r *Request) { r.ProtocolVersion++ },
		"revision": func(r *Request) { r.CapabilityRevision = "old" },
		"boot":     func(r *Request) { r.BootID = "" },
		"role":     func(r *Request) { r.Role = RoleSSHProof },
		"deadline": func(r *Request) { r.DeadlineAt = now.Add(3 * time.Minute).UnixMilli() },
		"digest":   func(r *Request) { r.PayloadDigest = strings.Repeat("0", 64) },
		"fence":    func(r *Request) { r.Fence.Sequence = 0 },
		"token":    func(r *Request) { r.Fence.Token = "raw-token" },
		"raw-id":   func(r *Request) { r.OperationID = "../../host" },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if request.Validate(now, definition) == nil {
				t.Fatal("invalid authority accepted")
			}
		})
	}
}

func TestBrokerJournalReplayFenceRecoveryAndRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "solovey-ui-broker")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := openTestJournal(root, "boot-one")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0).UTC()
	request := brokerMutationRequest(t, now, 1, "idem-one")
	peer := PeerIdentity{Revision: Digest([]byte("peer"))}
	digest := Digest(append(canonicalRequestAuthority(request), request.Payload...))
	replay, receipt, err := journal.Begin(request, peer, digest, now)
	if err != nil || replay != nil || receipt == nil {
		t.Fatalf("begin replay=%v receipt=%v err=%v", replay, receipt, err)
	}
	response, err := journal.Commit(request, receipt, successResponse(request), now.Add(time.Second))
	if err != nil || response.Receipt == nil {
		t.Fatal(err)
	}
	replay, _, err = journal.Begin(request, peer, digest, now.Add(2*time.Second))
	if err != nil || replay == nil || !replay.Replay {
		t.Fatalf("idempotent replay missing: %#v %v", replay, err)
	}
	conflict := request
	conflict.Payload = json.RawMessage(`{"value":"changed"}`)
	conflict.PayloadDigest = Digest(conflict.Payload)
	conflictDigest := Digest(append(canonicalRequestAuthority(conflict), conflict.Payload...))
	if _, _, err := journal.Begin(conflict, peer, conflictDigest, now); publicCode(err) != CodeIdempotency {
		t.Fatalf("conflicting replay code=%v err=%v", publicCode(err), err)
	}
	stale := brokerMutationRequest(t, now, 1, "idem-two")
	staleDigest := Digest(append(canonicalRequestAuthority(stale), stale.Payload...))
	if _, _, err := journal.Begin(stale, peer, staleDigest, now); publicCode(err) != CodeFence {
		t.Fatalf("stale fence code=%v err=%v", publicCode(err), err)
	}
	active := brokerMutationRequest(t, now, 2, "idem-active")
	activeDigest := Digest(append(canonicalRequestAuthority(active), active.Payload...))
	if _, _, err := journal.Begin(active, peer, activeDigest, now); err != nil {
		t.Fatal(err)
	}
	restarted, err := openTestJournal(root, "boot-two")
	if err != nil || len(restarted.Unresolved()) != 1 {
		t.Fatalf("restart unresolved=%v err=%v", restarted.Unresolved(), err)
	}
	if _, _, err := restarted.Begin(active, peer, activeDigest, now); publicCode(err) != CodeRecoveryRequired {
		t.Fatalf("unresolved replay code=%v err=%v", publicCode(err), err)
	}
	other := brokerMutationRequest(t, now, 3, "idem-other")
	otherDigest := Digest(append(canonicalRequestAuthority(other), other.Payload...))
	if _, _, err := restarted.Begin(other, peer, otherDigest, now); publicCode(err) != CodeRecoveryRequired {
		t.Fatalf("second writer bypassed unresolved authority code=%v err=%v", publicCode(err), err)
	}
}

func TestBrokerDispatchIsSingleWriterIdempotentAndRedacted(t *testing.T) {
	root := filepath.Join(t.TempDir(), "solovey-ui-broker")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := openTestJournal(root, "boot")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	var calls atomic.Int32
	verb := Verb("deployment.test.apply")
	if err := registry.Register(verb, Definition{Role: RolePanel, Mutation: true, Handler: func(_ context.Context, _ Request, _ PeerIdentity) (any, error) {
		calls.Add(1)
		return nil, errors.New("secret=/root/private argv=unsafe")
	}}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(registry, journal, StaticAttestor{Peer: PeerIdentity{Revision: Digest([]byte("peer"))}}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0).UTC()
	server.Now = func() time.Time { return now }
	request := brokerMutationRequest(t, now, 1, "idem-dispatch")
	request.Verb = verb
	first := server.Handle(context.Background(), request, PeerIdentity{Revision: Digest([]byte("peer"))})
	second := server.Handle(context.Background(), request, PeerIdentity{Revision: Digest([]byte("peer"))})
	if first.Code != CodeInternal || strings.Contains(first.Message, "root") || strings.Contains(first.Message, "argv") || !second.Replay || calls.Load() != 1 {
		t.Fatalf("first=%#v second=%#v calls=%d", first, second, calls.Load())
	}
	unknown := request
	unknown.Verb = "host.command.run"
	if response := server.Handle(context.Background(), unknown, PeerIdentity{}); response.Code != CodeUnsupported {
		t.Fatalf("unknown generic verb response=%#v", response)
	}
	drift := request
	drift.BootID = "other-boot"
	if response := server.Handle(context.Background(), drift, PeerIdentity{}); response.Code != CodeCapability {
		t.Fatalf("boot drift response=%#v", response)
	}
}

func TestProductionVerbVocabularyHasNoGenericEscapeHatch(t *testing.T) {
	verbs := []Verb{VerbCapabilities, VerbSSHObserve, VerbSSHStage, VerbSSHValidate, VerbSSHReload, VerbSSHArm, VerbSSHRestore,
		VerbSSHInspect, VerbSSHVerify, VerbSSHProof, VerbDeploymentObserve, VerbDeploymentDoctor, VerbDeploymentPrepare,
		VerbDeploymentApply, VerbDeploymentVerify, VerbDeploymentRollback}
	for _, verb := range verbs {
		value := string(verb)
		for _, forbidden := range []string{"command", "argv", "environment", "filesystem", "service.start", "service.stop", "docker"} {
			if strings.Contains(value, forbidden) {
				t.Fatalf("generic escape vocabulary in %q", verb)
			}
		}
	}
}

func TestActivationContractRequiresExactlyBothNamedDescriptors(t *testing.T) {
	for _, names := range []string{"main:proof", "proof:main"} {
		result, err := activatedDescriptorNames(42, 42, "2", names)
		if err != nil || len(result) != 2 {
			t.Fatalf("valid activation rejected names=%q result=%v err=%v", names, result, err)
		}
	}
	for _, invalid := range []struct {
		pid, current int
		count, names string
	}{
		{41, 42, "2", "main:proof"}, {42, 42, "1", "main"}, {42, 42, "3", "main:proof:extra"},
		{42, 42, "2", "main:main"}, {42, 42, "2", "main:tcp"}, {42, 42, "2", ""},
	} {
		if _, err := activatedDescriptorNames(invalid.pid, invalid.current, invalid.count, invalid.names); err == nil {
			t.Fatalf("unsafe activation accepted: %#v", invalid)
		}
	}
}

func TestManifestMatchingRejectsIdentityDriftAndAcceptsSetgidProof(t *testing.T) {
	digest := Digest([]byte("executable"))
	manifest := Manifest{Revision: Digest([]byte("manifest")), Clients: []ClientManifest{
		{Name: "panel", UID: 1001, GID: 1001, Executable: "/usr/local/solovey-ui/solovey-ui", ExecutableDigest: digest,
			Device: 7, Inode: 11, CgroupUnit: "solovey-ui.service", Roles: []Role{RolePanel}},
		{Name: "proof", AnyNonRootUID: true, AnyGID: true, RequiredGroup: 1001, Executable: "/usr/local/solovey-ui/solovey-ssh-proof",
			ExecutableDigest: digest, Device: 7, Inode: 12, Roles: []Role{RoleSSHProof}},
	}}
	panel := PeerIdentity{UID: 1001, GID: 1001, Executable: "/usr/local/solovey-ui/solovey-ui", ExecutableDigest: digest,
		Device: 7, Inode: 11, CgroupUnit: "solovey-ui.service"}
	if _, ok := manifest.matching(RolePanel, panel); !ok {
		t.Fatal("exact panel identity rejected")
	}
	for name, mutate := range map[string]func(*PeerIdentity){
		"uid": func(p *PeerIdentity) { p.UID++ }, "gid": func(p *PeerIdentity) { p.GID++ },
		"executable": func(p *PeerIdentity) { p.Executable = "/tmp/replaced" }, "digest": func(p *PeerIdentity) { p.ExecutableDigest = Digest([]byte("replaced")) },
		"device": func(p *PeerIdentity) { p.Device++ }, "inode": func(p *PeerIdentity) { p.Inode++ }, "cgroup": func(p *PeerIdentity) { p.CgroupUnit = "wrong.service" },
	} {
		t.Run(name, func(t *testing.T) {
			drift := panel
			mutate(&drift)
			if _, ok := manifest.matching(RolePanel, drift); ok {
				t.Fatal("identity drift accepted")
			}
		})
	}
	proof := PeerIdentity{UID: 2002, GID: 1001, Executable: "/usr/local/solovey-ui/solovey-ssh-proof", ExecutableDigest: digest, Device: 7, Inode: 12}
	if _, ok := manifest.matching(RoleSSHProof, proof); !ok {
		t.Fatal("setgid proof identity rejected")
	}
	proof.GID, proof.Groups = 2002, []uint32{1001}
	if _, ok := manifest.matching(RoleSSHProof, proof); !ok {
		t.Fatal("supplementary proof group rejected")
	}
	proof.Groups = nil
	if _, ok := manifest.matching(RoleSSHProof, proof); ok {
		t.Fatal("proof without required group accepted")
	}
	if _, ok := manifest.matching(RolePanel, panel); !ok {
		t.Fatal("manifest mutated during matching")
	}
}

func TestBrokerAuditIsSafeAndAggregatesRepeatedDenials(t *testing.T) {
	registry := NewRegistry()
	peer := PeerIdentity{Revision: Digest([]byte("peer"))}
	server, err := NewServer(registry, memoryJournal{}, StaticAttestor{Peer: peer}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0).UTC()
	server.Now = func() time.Time { return now }
	var events []AuditEvent
	server.Audit = func(event AuditEvent) { events = append(events, event) }
	request := brokerMutationRequest(t, now, 1, "idem-audit")
	request.Verb = "host.command.run"
	request.Purpose = "secret-do-not-log"
	for range 7 {
		response := server.Handle(context.Background(), request, peer)
		if response.Code != CodeUnsupported {
			t.Fatalf("unexpected response: %#v", response)
		}
	}
	if len(events) != 3 || events[0].AggregateCount != 1 || events[1].AggregateCount != 2 || events[2].AggregateCount != 4 {
		t.Fatalf("denial aggregation is not bounded: %#v", events)
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-do-not-log", "payload", "/root", "argv", "environment"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("audit leaked forbidden value %q: %s", forbidden, encoded)
		}
	}
}

func TestBrokerAuditNormalizesUnknownResultCodes(t *testing.T) {
	server := &Server{}
	event := server.auditEvent(Request{}, PeerIdentity{}, Response{Code: ErrorCode("attacker-value")}, time.Millisecond)
	if event.ResultClass != "denied_"+string(CodeInternal) {
		t.Fatalf("unexpected audit result class %q", event.ResultClass)
	}
}

func TestBrokerDeadlineCancellationCapabilitiesAndResponseBounds(t *testing.T) {
	registry := NewRegistry()
	waitVerb := Verb("deployment.test.observe")
	if err := registry.Register(waitVerb, Definition{Role: RolePanel, Handler: func(ctx context.Context, _ Request, _ PeerIdentity) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}); err != nil {
		t.Fatal(err)
	}
	largeVerb := Verb("deployment.large.observe")
	if err := registry.Register(largeVerb, Definition{Role: RolePanel, Handler: func(context.Context, Request, PeerIdentity) (any, error) {
		return strings.Repeat("x", MaxResponseBytes), nil
	}}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(registry, memoryJournal{}, StaticAttestor{Peer: PeerIdentity{Revision: Digest([]byte("peer"))}}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	if cap(server.limit) != 32 {
		t.Fatalf("connection bound=%d", cap(server.limit))
	}
	now := time.Now().UTC()
	server.Now = func() time.Time { return now }
	request := brokerReadRequest(t, now, waitVerb)
	request.DeadlineAt = time.Now().Add(20 * time.Millisecond).UnixMilli()
	if response := server.Handle(context.Background(), request, PeerIdentity{}); response.Code != CodeDeadline {
		t.Fatalf("deadline response=%#v", response)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	request.DeadlineAt = time.Now().Add(time.Minute).UnixMilli()
	if response := server.Handle(canceled, request, PeerIdentity{}); response.Code != CodeDeadline {
		t.Fatalf("cancellation response=%#v", response)
	}
	request = brokerReadRequest(t, now, largeVerb)
	if response := server.Handle(context.Background(), request, PeerIdentity{}); response.Code != CodeInternal || strings.Contains(response.Message, "xxx") {
		t.Fatalf("response bound=%#v", response)
	}
	request = brokerReadRequest(t, now, VerbCapabilities)
	var result CapabilitiesV1
	response := server.Handle(context.Background(), request, PeerIdentity{})
	if !response.OK || DecodePayload(response.Payload, &result) != nil || result.ProtocolVersion != ProtocolVersion || result.CapabilityRevision != CapabilityRevision || len(result.Verbs) != 3 {
		t.Fatalf("capability negotiation response=%#v result=%#v", response, result)
	}
}

func brokerMutationRequest(t *testing.T, now time.Time, sequence uint64, idempotency string) Request {
	t.Helper()
	payload, payloadDigest, err := MarshalPayload(protocolFixture{Value: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	return Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, BootID: "boot", Role: RolePanel,
		Verb: "deployment.test.apply", RequestID: "request-one", OperationID: "operation-one", IdempotencyKey: idempotency,
		Fence:      Fence{Resource: "deployment", Sequence: sequence, Token: Digest([]byte("token"))},
		DeadlineAt: now.Add(time.Minute).UnixMilli(), Payload: payload, PayloadDigest: payloadDigest}
}

func brokerReadRequest(t *testing.T, now time.Time, verb Verb) Request {
	t.Helper()
	payload, payloadDigest, err := MarshalPayload(struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return Request{ProtocolVersion: ProtocolVersion, CapabilityRevision: CapabilityRevision, BootID: "boot", Role: RolePanel,
		Verb: verb, RequestID: "request-read", OperationID: "operation-read", DeadlineAt: now.Add(time.Minute).UnixMilli(),
		Payload: payload, PayloadDigest: payloadDigest}
}

func publicCode(err error) ErrorCode {
	code, _ := publicFailure(err)
	return code
}

func openTestJournal(root, bootID string) (*FileJournal, error) {
	journal := &FileJournal{root: root, path: filepath.Join(root, journalFileName), bootID: bootID,
		replays: make(map[string]replayRecord), fences: make(map[string]uint64), unresolved: make(map[string]Receipt)}
	file, err := os.Open(journal.path)
	if errors.Is(err, os.ErrNotExist) {
		return journal, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := journal.loadRows(file); err != nil {
		return nil, err
	}
	return journal, nil
}
