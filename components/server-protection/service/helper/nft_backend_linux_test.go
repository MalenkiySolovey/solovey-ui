//go:build linux

package helper

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSystemNFTExecutorUsesCommonTextCLIContract(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned Linux executable fixture is required")
	}
	root, err := os.MkdirTemp("/run", "solovey-nft-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "operations.log")
	revision := strings.Repeat("a", 64)
	liveTable := "table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    counter accept\n  }\n}\n"
	scriptPath := filepath.Join(root, "nft")
	script := "#!/bin/sh\n" +
		"set -eu\n" +
		"printf '%s\\n' \"$*\" >> " + shellSingleQuote(logPath) + "\n" +
		"case \"$*\" in\n" +
		"  --version) printf '%s\\n' 'nftables v1.1.6' ;;\n" +
		"  'list tables') ;;\n" +
		"  '--check --file -') cat >/dev/null ;;\n" +
		"  '-c -f '*) test -f \"$3\" ;;\n" +
		"  'list table inet solovey_protection') printf '%s' " + shellSingleQuote(liveTable) + " ;;\n" +
		"  '-f '*) test -f \"$2\" ;;\n" +
		"  *) exit 64 ;;\n" +
		"esac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o555); err != nil {
		t.Fatal(err)
	}
	binary := openNFTExecutable([]string{scriptPath})
	if binary == nil {
		t.Fatal("trusted common nft CLI fixture was rejected")
	}
	t.Cleanup(func() { _ = binary.Close() })
	executor := systemNFTExecutor{binary: binary}
	if support := executor.Detect(t.Context()); !support.Available || !support.TTLSet || !support.RateLimit {
		t.Fatalf("common text CLI capability detection failed: %#v", support)
	}
	candidatePath := filepath.Join(root, "candidate.nft")
	if err := os.WriteFile(candidatePath, []byte(liveTable), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.CheckManagedFile(t.Context(), candidatePath); err != nil {
		t.Fatal(err)
	}
	listed, present, err := executor.ListManagedTable(t.Context())
	if err != nil || !present || string(listed) != liveTable {
		t.Fatalf("text managed-table observation: present=%v err=%v output=%q", present, err, listed)
	}
	observed, err := observeManagedTable(t.Context(), executor)
	if err != nil || !observed.present || observed.revision != revision || observed.semanticSHA == "" {
		t.Fatalf("text semantic observation failed: observation=%#v err=%v", observed, err)
	}
	if err := executor.ApplyManagedFile(t.Context(), candidatePath); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--version",
		"list tables",
		"--check --file -",
		"--check --file -",
		"-c -f " + candidatePath,
		"list table inet solovey_protection",
		"list table inet solovey_protection",
		"-f " + candidatePath,
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("production nft operation vocabulary differs:\n got: %q\nwant: %q", got, want)
	}
	for _, operation := range got {
		for _, argument := range strings.Fields(operation) {
			if argument == "-j" || argument == "--json" || strings.Contains(argument, "json") {
				t.Fatalf("production nft operation requested JSON capability: %q", operation)
			}
		}
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// TestManagedNFTHostSequence is the opt-in real-kernel gate. The caller must
// place the test binary in a disposable network namespace; the test touches
// only inet solovey_protection and never flushes the namespace ruleset.
func TestManagedNFTHostSequence(t *testing.T) {
	if os.Getenv("SUI_NFT_HOST_TESTS") != "1" {
		t.Skip("set SUI_NFT_HOST_TESTS=1 inside a disposable Linux network namespace")
	}
	root := testManagedRoot(t)
	executor, ok := newSystemNFTExecutor().(systemNFTExecutor)
	if !ok || executor.binary == nil {
		t.Fatal("trusted nft executable is unavailable")
	}
	if support := executor.Detect(t.Context()); !support.Available {
		t.Fatalf("real nft capability unavailable: %#v", support)
	}
	if _, _, err := executor.run(t.Context(), "add", "table", "inet", "foreign_external_a"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _, _ = executor.run(t.Context(), "delete", "table", "inet", "solovey_protection")
		_, _, _ = executor.run(t.Context(), "delete", "table", "inet", "foreign_external_a")
	})
	engine := newContractEngineWithExecutor(root, executor)
	revisionA, revisionB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	candidateA := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revisionA + "\"\n  set solovey_allow_tcp_ports {\n    type inet_service\n    elements = { 22 }\n  }\n  set solovey_block4_aaaaaaaaaaaa {\n    type ipv4_addr\n    flags interval,timeout\n    size 4096\n    timeout 3600s\n    elements = { 192.0.2.1 timeout 300s }\n  }\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    counter accept\n  }\n  chain solovey_endpoint_aaaaaaaaaaaa {\n    ip saddr @solovey_block4_aaaaaaaaaaaa counter drop\n    counter accept\n  }\n}\n")
	candidateB := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revisionB + "\"\n  set solovey_allow_tcp_ports {\n    type inet_service\n    elements = { 22, 443 }\n  }\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    iifname \"lo\" counter accept\n    counter accept\n  }\n}\n")
	semanticA, err := ManagedSemanticSHA256(candidateA)
	if err != nil {
		t.Fatal(err)
	}
	semanticB, err := ManagedSemanticSHA256(candidateB)
	if err != nil {
		t.Fatal(err)
	}
	membershipA, err := ManagedTimedMembershipSHA256(candidateA)
	if err != nil {
		t.Fatal(err)
	}
	membershipB, err := ManagedTimedMembershipSHA256(candidateB)
	if err != nil {
		t.Fatal(err)
	}
	writeTestManagedFile(t, root, "revisions/host-a/candidate.nft", candidateA)
	writeTestManagedFile(t, root, "revisions/host-b/candidate.nft", candidateB)
	corrA := Correlation{OperationID: "host-sequence-a", InstanceID: "host-test", LockRevision: 1}
	validatedA := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: corrA, Operation: OperationNFTValidate,
		NFTValidate: &NFTValidateRequest{CandidatePath: "revisions/host-a/candidate.nft", ExpectedRevision: revisionA, ExpectedSHA256: sha256Hex(candidateA), ExpectedSemanticSHA256: semanticA, ExpectedTimedMembershipSHA256: membershipA}})
	if !validatedA.OK || validatedA.NFT == nil || validatedA.NFT.PreviousTablePresent || validatedA.NFT.SemanticSHA256 != semanticA {
		t.Fatalf("candidate A validation failed: %#v", validatedA)
	}
	appliedA := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: corrA, Operation: OperationNFTApply,
		NFTApply: &NFTApplyRequest{CandidatePath: "revisions/host-a/candidate.nft", RollbackArtifactPath: "revisions/host-a/firewall-before.nft", ExpectedTable: managedTable,
			ExpectedRevision: revisionA, ExpectedSHA256: sha256Hex(candidateA), ExpectedSemanticSHA256: semanticA, ExpectedTimedMembershipSHA256: membershipA}})
	if !appliedA.OK || appliedA.NFT == nil || appliedA.NFT.SemanticSHA256 != semanticA {
		t.Fatalf("candidate A real apply/list verification failed: %#v", appliedA)
	}
	observedA, err := engine.observe(t.Context())
	if err != nil || observedA.CurrentSemanticSHA256 != semanticA || observedA.CurrentTimedMembershipSHA256 != membershipA {
		t.Fatalf("candidate A real semantic list failed: result=%#v err=%v", observedA, err)
	}
	if _, _, err := executor.runWithInput(t.Context(), []byte("add element inet solovey_protection solovey_block4_aaaaaaaaaaaa { 198.51.100.2 timeout 1s }\n"), "-f", "-"); err != nil {
		t.Fatal(err)
	}
	mutable, err := engine.observe(t.Context())
	if err != nil || mutable.CurrentSemanticSHA256 != semanticA || mutable.CurrentTimedMembershipSHA256 == membershipA {
		t.Fatalf("unexpected timed-set insertion was not separated from static identity: result=%#v err=%v", mutable, err)
	}
	time.Sleep(1200 * time.Millisecond)
	aged, err := engine.observe(t.Context())
	if err != nil || aged.CurrentSemanticSHA256 != semanticA || aged.CurrentTimedMembershipSHA256 != membershipA {
		t.Fatalf("normal timeout aging/expiry changed static identity: result=%#v err=%v", aged, err)
	}
	if _, _, err := executor.runWithInput(t.Context(), []byte("delete element inet solovey_protection solovey_block4_aaaaaaaaaaaa { 192.0.2.1 }\n"), "-f", "-"); err != nil {
		t.Fatal(err)
	}
	lost, err := engine.observe(t.Context())
	if err != nil || lost.CurrentSemanticSHA256 != semanticA || lost.CurrentTimedMembershipSHA256 == membershipA {
		t.Fatalf("premature timed-set membership loss was not detected: result=%#v err=%v", lost, err)
	}
	if _, _, err := executor.runWithInput(t.Context(), []byte("add element inet solovey_protection solovey_block4_aaaaaaaaaaaa { 192.0.2.1 timeout 300s }\n"), "-f", "-"); err != nil {
		t.Fatal(err)
	}

	corrB := Correlation{OperationID: "host-sequence-b", InstanceID: "host-test", LockRevision: 2}
	validatedB := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: corrB, Operation: OperationNFTValidate,
		NFTValidate: &NFTValidateRequest{CandidatePath: "revisions/host-b/candidate.nft", ExpectedRevision: revisionB, ExpectedSHA256: sha256Hex(candidateB), ExpectedSemanticSHA256: semanticB, ExpectedTimedMembershipSHA256: membershipB}})
	if !validatedB.OK || validatedB.NFT == nil || !validatedB.NFT.PreviousTablePresent || validatedB.NFT.PreviousSemanticSHA256 != semanticA {
		t.Fatalf("candidate B pre-mutation observation failed: %#v", validatedB)
	}
	appliedB := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: corrB, Operation: OperationNFTApply,
		NFTApply: &NFTApplyRequest{CandidatePath: "revisions/host-b/candidate.nft", RollbackArtifactPath: "revisions/host-b/firewall-before.nft", ExpectedTable: managedTable,
			ExpectedRevision: revisionB, ExpectedSHA256: sha256Hex(candidateB), ExpectedSemanticSHA256: semanticB, ExpectedTimedMembershipSHA256: membershipB,
			ExpectedPreviousRevision: revisionA, ExpectedPreviousSemanticSHA256: semanticA, ExpectedPreviousTimedMembershipSHA256: membershipA, ExpectedPreviousTablePresent: true}})
	if !appliedB.OK || appliedB.NFT == nil || appliedB.NFT.SemanticSHA256 != semanticB {
		t.Fatalf("candidate B real apply/list verification failed: %#v", appliedB)
	}
	observedB, err := engine.observe(t.Context())
	if err != nil || observedB.CurrentSemanticSHA256 != semanticB {
		t.Fatalf("candidate B real semantic list failed: result=%#v err=%v", observedB, err)
	}
	rolledBack := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: corrB, Operation: OperationNFTRollback,
		NFTRollback: &NFTRollbackRequest{RollbackArtifactPath: "revisions/host-b/firewall-before.nft", ExpectedTable: managedTable,
			ExpectedSHA256: appliedB.NFT.RollbackSHA256, ExpectedCurrentRevision: revisionB, ExpectedCurrentSemanticSHA256: semanticB, ExpectedCurrentTimedMembershipSHA256: membershipB}})
	if !rolledBack.OK || rolledBack.NFT == nil || !rolledBack.NFT.ManagedTablePresent || rolledBack.NFT.CurrentSemanticSHA256 != semanticA {
		t.Fatalf("real rollback did not restore A: %#v", rolledBack)
	}
	observedRollback, err := engine.observe(t.Context())
	if err != nil || observedRollback.CurrentSemanticSHA256 != semanticA {
		t.Fatalf("candidate A real semantic list after rollback failed: result=%#v err=%v", observedRollback, err)
	}
	replayedRollback := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: corrB, Operation: OperationNFTRollback,
		NFTRollback: &NFTRollbackRequest{RollbackArtifactPath: "revisions/host-b/firewall-before.nft", ExpectedTable: managedTable,
			ExpectedSHA256: appliedB.NFT.RollbackSHA256, ExpectedCurrentRevision: revisionB, ExpectedCurrentSemanticSHA256: semanticB, ExpectedCurrentTimedMembershipSHA256: membershipB}})
	if !replayedRollback.OK || replayedRollback.NFT == nil || replayedRollback.NFT.CurrentSemanticSHA256 != semanticA {
		t.Fatalf("real rollback retry was not idempotent: %#v", replayedRollback)
	}
	if _, _, err := executor.runWithInput(t.Context(), []byte("add chain inet solovey_protection foreign_extra { counter accept; }\n"), "-f", "-"); err != nil {
		t.Fatal(err)
	}
	extraChain, err := engine.observe(t.Context())
	if err != nil || extraChain.CurrentSemanticSHA256 == semanticA {
		t.Fatalf("foreign extra chain was not detected: result=%#v err=%v", extraChain, err)
	}
	if _, _, err := executor.run(t.Context(), "delete", "chain", "inet", "solovey_protection", "foreign_extra"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executor.runWithInput(t.Context(), []byte("add set inet solovey_protection foreign_extra { type ipv4_addr; }\n"), "-f", "-"); err != nil {
		t.Fatal(err)
	}
	extraSet, err := engine.observe(t.Context())
	if err != nil || extraSet.CurrentSemanticSHA256 == semanticA {
		t.Fatalf("foreign extra set was not detected: result=%#v err=%v", extraSet, err)
	}
	if _, _, err := executor.run(t.Context(), "delete", "set", "inet", "solovey_protection", "foreign_extra"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := executor.run(t.Context(), "add", "rule", "inet", "solovey_protection", "solovey_input", "counter", "drop"); err != nil {
		t.Fatal(err)
	}
	drifted, err := engine.observe(t.Context())
	if err != nil || drifted.CurrentSemanticSHA256 == semanticA {
		t.Fatalf("semantic drift was not detected: result=%#v err=%v", drifted, err)
	}
	if _, _, err := executor.run(t.Context(), "delete", "table", "inet", "solovey_protection"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executor.runWithInput(t.Context(), []byte("table inet solovey_protection {\n}\n"), "-f", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.observe(t.Context()); err == nil {
		t.Fatal("foreign same-name table was accepted as owned")
	}
	if _, _, err := executor.run(t.Context(), "delete", "table", "inet", "solovey_protection"); err != nil {
		t.Fatal(err)
	}
	absent, err := engine.observe(t.Context())
	if err != nil || absent.ManagedTablePresent {
		t.Fatalf("managed table deletion was not observed as ABSENT: result=%#v err=%v", absent, err)
	}
	if _, _, err := executor.run(t.Context(), "list", "table", "inet", "foreign_external_a"); err != nil {
		t.Fatalf("foreign table did not survive managed apply/change/rollback: %v", err)
	}
}

// TestManagedNFTHostNearAdmissionLimit proves that the mandatory live reader
// can observe an accepted writer state whose generated artifact is close to
// the admission ceiling. The caller supplies the disposable network namespace
// used by the other opt-in host sequence.
func TestManagedNFTHostNearAdmissionLimit(t *testing.T) {
	if os.Getenv("SUI_NFT_HOST_TESTS") != "1" {
		t.Skip("set SUI_NFT_HOST_TESTS=1 inside a disposable Linux network namespace")
	}
	root := testManagedRoot(t)
	executor, ok := newSystemNFTExecutor().(systemNFTExecutor)
	if !ok || executor.binary == nil {
		t.Fatal("trusted nft executable is unavailable")
	}
	if support := executor.Detect(t.Context()); !support.Available {
		t.Fatalf("real nft capability unavailable: %#v", support)
	}
	t.Cleanup(func() {
		_, _, _ = executor.run(t.Context(), "delete", "table", "inet", "solovey_protection")
	})

	revision := strings.Repeat("c", 64)
	candidate := largeTimedManagedCandidate(revision)
	if len(candidate) < MaxManagedCandidateBytes*7/8 || len(candidate) > MaxManagedCandidateBytes {
		t.Fatalf("large candidate size = %d, admission ceiling = %d", len(candidate), MaxManagedCandidateBytes)
	}
	semantic, err := ManagedSemanticSHA256(candidate)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := ManagedTimedMembershipSHA256(candidate)
	if err != nil {
		t.Fatal(err)
	}
	writeTestManagedFile(t, root, "revisions/host-large/candidate.nft", candidate)
	correlation := Correlation{OperationID: "host-sequence-large", InstanceID: "host-test", LockRevision: 1}
	engine := newContractEngineWithExecutor(root, executor)
	validated := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTValidate,
		NFTValidate: &NFTValidateRequest{CandidatePath: "revisions/host-large/candidate.nft", ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: semantic, ExpectedTimedMembershipSHA256: membership}})
	if !validated.OK || validated.NFT == nil || validated.NFT.SemanticSHA256 != semantic || validated.NFT.TimedMembershipSHA256 != membership {
		t.Fatalf("near-limit candidate validation failed: %#v", validated)
	}
	applied := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply,
		NFTApply: &NFTApplyRequest{CandidatePath: "revisions/host-large/candidate.nft", RollbackArtifactPath: "revisions/host-large/firewall-before.nft", ExpectedTable: managedTable,
			ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: semantic, ExpectedTimedMembershipSHA256: membership}})
	if !applied.OK || applied.NFT == nil || applied.NFT.SemanticSHA256 != semantic || applied.NFT.TimedMembershipSHA256 != membership {
		t.Fatalf("near-limit candidate apply/list verification failed: %#v", applied)
	}
	observed, err := engine.observe(t.Context())
	if err != nil || observed.CurrentSemanticSHA256 != semantic || observed.CurrentTimedMembershipSHA256 != membership {
		t.Fatalf("near-limit mandatory observation failed: result=%#v err=%v", observed, err)
	}
}

func TestManagedNFTHostExternalFlushAndPackageRemoval(t *testing.T) {
	if os.Getenv("SUI_NFT_HOST_TESTS") != "1" {
		t.Skip("set SUI_NFT_HOST_TESTS=1 inside a disposable Linux network namespace")
	}
	executor, ok := newSystemNFTExecutor().(systemNFTExecutor)
	if !ok || executor.binary == nil {
		t.Fatal("trusted nft executable is unavailable")
	}
	if support := executor.Detect(t.Context()); !support.Available {
		t.Fatalf("real nft capability unavailable: %#v", support)
	}
	t.Cleanup(func() {
		_, _, _ = executor.run(t.Context(), "delete", "table", "inet", "solovey_protection")
		_, _, _ = executor.run(t.Context(), "delete", "table", "inet", "foreign_external_b")
	})
	revision := strings.Repeat("d", 64)
	managed := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    counter accept\n  }\n}\n")
	foreign := []byte("table inet foreign_external_b {\n  chain input {\n    counter accept\n  }\n}\n")
	apply := func() {
		t.Helper()
		if _, _, err := executor.runWithInput(t.Context(), append(append([]byte(nil), managed...), foreign...), "-f", "-"); err != nil {
			t.Fatal(err)
		}
	}
	apply()

	// Exact pinned fw4 flush semantics enumerate all nft tables and delete each
	// one. The Solovey observer must consume the resulting external loss; it
	// must not recreate or claim any table while observing absence.
	tables, _, err := executor.run(t.Context(), "list", "tables")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(tables)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "table" {
			t.Fatalf("unexpected nft table inventory line %q", line)
		}
		if _, _, err := executor.run(t.Context(), "delete", "table", fields[1], fields[2]); err != nil {
			t.Fatal(err)
		}
	}
	observation, err := observeManagedTable(t.Context(), executor)
	if err != nil || observation.present {
		t.Fatalf("full external flush was not observed as absent: observation=%#v err=%v", observation, err)
	}
	if _, present, err := executor.ListManagedTable(t.Context()); err != nil || present {
		t.Fatalf("absence observation recreated the managed table: present=%v err=%v", present, err)
	}

	apply()
	foreignBefore, _, err := executor.run(t.Context(), "list", "table", "inet", "foreign_external_b")
	if err != nil {
		t.Fatal(err)
	}
	if err := removeManagedTableForPackageRemoval(t.Context(), executor); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedTableForPackageRemoval(t.Context(), executor); err != nil {
		t.Fatalf("repeated package cleanup failed: %v", err)
	}
	if _, present, err := executor.ListManagedTable(t.Context()); err != nil || present {
		t.Fatalf("package cleanup did not leave the managed table absent: present=%v err=%v", present, err)
	}
	foreignAfter, _, err := executor.run(t.Context(), "list", "table", "inet", "foreign_external_b")
	if err != nil || string(foreignAfter) != string(foreignBefore) {
		t.Fatalf("package cleanup changed foreign table: err=%v\nbefore=%s\nafter=%s", err, foreignBefore, foreignAfter)
	}
}

// TestManagedNFTHostEndpointTemporalSemantics proves the two Endpoint time contracts in
// a disposable network namespace: an absolute packet-time recovery exemption
// expires without a ruleset rewrite while a permanent trusted source survives,
// and rollback never restarts a captured relative set-element lifetime.
func TestManagedNFTHostEndpointTemporalSemantics(t *testing.T) {
	if os.Getenv("SUI_NFT_HOST_TESTS") != "1" {
		t.Skip("set SUI_NFT_HOST_TESTS=1 inside a disposable Linux network namespace")
	}
	root := testManagedRoot(t)
	executor, ok := newSystemNFTExecutor().(systemNFTExecutor)
	if !ok || executor.binary == nil {
		t.Fatal("trusted nft executable is unavailable")
	}
	if support := executor.Detect(t.Context()); !support.Available || !support.TTLSet {
		t.Fatalf("real nft temporal capability unavailable: %#v", support)
	}
	t.Cleanup(func() { _, _, _ = executor.run(t.Context(), "delete", "table", "inet", "solovey_protection") })
	engine := newContractEngineWithExecutor(root, executor)

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	expiresAt := time.Now().UTC().Add(3 * time.Second).Unix()
	revision := strings.Repeat("e", 64)
	candidate := []byte(fmt.Sprintf("table inet solovey_protection {\n  comment \"solovey-revision:%s\"\n  set solovey_block4_eeeeeeeeeeee {\n    type ipv4_addr\n    flags interval\n    elements = { 127.0.0.0/8 }\n  }\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    meta nfproto ipv4 ip saddr 127.0.0.2/32 ip daddr 127.0.0.1 meta l4proto tcp tcp dport %d meta time < %d counter accept\n    meta nfproto ipv4 ip saddr 127.0.0.3/32 ip daddr 127.0.0.1 meta l4proto tcp tcp dport %d counter accept\n    meta nfproto ipv4 ip daddr 127.0.0.1 meta l4proto tcp tcp dport %d jump solovey_endpoint_eeeeeeeeeeee\n  }\n  chain solovey_endpoint_eeeeeeeeeeee {\n    ip saddr @solovey_block4_eeeeeeeeeeee counter drop\n    counter accept\n  }\n}\n", revision, port, expiresAt, port, port))
	writeTestManagedFile(t, root, "revisions/endpoint-exemption/candidate.nft", candidate)
	candidatePath, err := root.Resolve("revisions/endpoint-exemption/candidate.nft", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCandidate(candidate, revision, sha256Hex(candidate)); err != nil {
		t.Fatalf("absolute-time candidate failed generated grammar: %v", err)
	}
	if err := executor.CheckManagedFile(t.Context(), candidatePath); err != nil {
		t.Fatalf("absolute-time candidate failed real nft check: %v\n%s", err, candidate)
	}
	semantic, err := ManagedSemanticSHA256(candidate)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := ManagedTimedMembershipSHA256(candidate)
	if err != nil {
		t.Fatal(err)
	}
	correlation := Correlation{OperationID: "endpoint-exemption", InstanceID: "host-test", LockRevision: 1}
	applied := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{
		CandidatePath: "revisions/endpoint-exemption/candidate.nft", RollbackArtifactPath: "revisions/endpoint-exemption/firewall-before.nft", ExpectedTable: managedTable,
		ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: semantic, ExpectedTimedMembershipSHA256: membership,
	}})
	if !applied.OK || applied.NFT == nil || applied.NFT.SemanticSHA256 != semantic {
		raw, present, listErr := executor.ListManagedTable(t.Context())
		t.Fatalf("absolute-time candidate did not survive real apply/list normalization: response=%#v present=%v listErr=%v\n%s", applied, present, listErr, raw)
	}
	connect := func(source string) error {
		ctx, cancel := context.WithTimeout(t.Context(), 700*time.Millisecond)
		defer cancel()
		dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}, Timeout: 600 * time.Millisecond}
		connection, dialErr := dialer.DialContext(ctx, "tcp4", listener.Addr().String())
		if connection != nil {
			_ = connection.Close()
		}
		return dialErr
	}
	if err := connect("127.0.0.2"); err != nil {
		t.Fatalf("temporary recovery source was not accepted before expiry: %v", err)
	}
	deadline := time.Unix(expiresAt, 0).Add(250 * time.Millisecond)
	if wait := time.Until(deadline); wait > 0 {
		time.Sleep(wait)
	}
	if err := connect("127.0.0.2"); err == nil {
		t.Fatal("temporary recovery source remained effective after absolute expiry")
	}
	if err := connect("127.0.0.3"); err != nil {
		t.Fatalf("permanent trusted source did not survive temporary expiry: %v", err)
	}
	if _, _, err := executor.run(t.Context(), "delete", "table", "inet", "solovey_protection"); err != nil {
		t.Fatal(err)
	}

	revisionA, revisionB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	candidateA := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revisionA + "\"\n  set solovey_block4_aaaaaaaaaaaa {\n    type ipv4_addr\n    flags interval,timeout\n    size 4096\n    timeout 60s\n    elements = { 192.0.2.1 timeout 4s }\n  }\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    counter accept\n  }\n}\n")
	candidateB := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revisionB + "\"\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    counter accept\n  }\n}\n")
	semanticA, _ := ManagedSemanticSHA256(candidateA)
	membershipA, _ := ManagedTimedMembershipSHA256(candidateA)
	semanticB, _ := ManagedSemanticSHA256(candidateB)
	membershipB, _ := ManagedTimedMembershipSHA256(candidateB)
	writeTestManagedFile(t, root, "revisions/endpoint-deadline-a/candidate.nft", candidateA)
	writeTestManagedFile(t, root, "revisions/endpoint-deadline-b/candidate.nft", candidateB)
	started := time.Now()
	applyA := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "endpoint-deadline-a", InstanceID: "host-test", LockRevision: 2}, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{
		CandidatePath: "revisions/endpoint-deadline-a/candidate.nft", RollbackArtifactPath: "revisions/endpoint-deadline-a/firewall-before.nft", ExpectedTable: managedTable,
		ExpectedRevision: revisionA, ExpectedSHA256: sha256Hex(candidateA), ExpectedSemanticSHA256: semanticA, ExpectedTimedMembershipSHA256: membershipA,
	}})
	if !applyA.OK {
		t.Fatalf("timed candidate A apply failed: %#v", applyA)
	}
	applyB := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "endpoint-deadline-b", InstanceID: "host-test", LockRevision: 3}, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{
		CandidatePath: "revisions/endpoint-deadline-b/candidate.nft", RollbackArtifactPath: "revisions/endpoint-deadline-b/firewall-before.nft", ExpectedTable: managedTable,
		ExpectedRevision: revisionB, ExpectedSHA256: sha256Hex(candidateB), ExpectedSemanticSHA256: semanticB, ExpectedTimedMembershipSHA256: membershipB,
		ExpectedPreviousRevision: revisionA, ExpectedPreviousSemanticSHA256: semanticA, ExpectedPreviousTimedMembershipSHA256: membershipA, ExpectedPreviousTablePresent: true,
	}})
	if !applyB.OK || applyB.NFT == nil {
		t.Fatalf("timed candidate B apply failed: %#v", applyB)
	}
	time.Sleep(2 * time.Second)
	rolled := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "endpoint-deadline-b", InstanceID: "host-test", LockRevision: 3}, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{
		RollbackArtifactPath: "revisions/endpoint-deadline-b/firewall-before.nft", ExpectedTable: managedTable, ExpectedSHA256: applyB.NFT.RollbackSHA256,
		ExpectedCurrentRevision: revisionB, ExpectedCurrentSemanticSHA256: semanticB, ExpectedCurrentTimedMembershipSHA256: membershipB,
	}})
	if !rolled.OK || rolled.NFT == nil || rolled.NFT.CurrentTimedMembershipSHA256 != membershipA {
		t.Fatalf("timed rollback did not restore the still-live member: %#v", rolled)
	}
	if wait := time.Until(started.Add(4500 * time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	emptyA := []byte(strings.Replace(string(candidateA), "elements = { 192.0.2.1 timeout 4s }", "elements = { }", 1))
	emptyMembership, err := ManagedTimedMembershipSHA256(emptyA)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := engine.observe(t.Context())
	if err != nil || observed.CurrentRevision != revisionA || observed.CurrentSemanticSHA256 != semanticA || observed.CurrentTimedMembershipSHA256 != emptyMembership {
		t.Fatalf("rollback resurrected the member beyond its original deadline: result=%#v err=%v", observed, err)
	}
	_ = listener.Close()
	<-done
}

func largeTimedManagedCandidate(revision string) []byte {
	const prefix = "table inet solovey_protection {\n  comment \"solovey-revision:%s\"\n  set solovey_block4_cccccccccccc {\n    type ipv4_addr\n    flags interval,timeout\n    size 99999\n    timeout 3600s\n    elements = { "
	const suffix = " }\n  }\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    counter accept\n  }\n  chain solovey_endpoint_cccccccccccc {\n    ip saddr @solovey_block4_cccccccccccc counter drop\n    counter accept\n  }\n}\n"
	var output strings.Builder
	output.Grow(MaxManagedCandidateBytes)
	output.WriteString(fmt.Sprintf(prefix, revision))
	for index := 0; ; index++ {
		member := fmt.Sprintf("10.%d.%d.%d timeout 300s", index/(256*128), (index/128)%256, (index%128)*2+1)
		separator := ""
		if index > 0 {
			separator = ", "
		}
		if output.Len()+len(separator)+len(member)+len(suffix) > MaxManagedCandidateBytes-1024 {
			break
		}
		output.WriteString(separator)
		output.WriteString(member)
	}
	output.WriteString(suffix)
	return []byte(output.String())
}
