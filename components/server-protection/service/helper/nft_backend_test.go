package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNFTAdvancedCapabilitiesRequireIndependentReadOnlyCheckProof(t *testing.T) {
	unproven := nftSupportFromPrimitiveChecks("nftables v1", errors.New("ttl"), errors.New("rate"))
	if unproven.TTLSet || unproven.RateLimit || unproven.TTLSetReason == "" || unproven.RateLimitReason == "" {
		t.Fatalf("unproven primitives were reported supported: %#v", unproven)
	}
	ttlOnly := nftSupportFromPrimitiveChecks("nftables v1", nil, errors.New("rate"))
	if !ttlOnly.TTLSet || ttlOnly.RateLimit || ttlOnly.RateLimitReason == "" {
		t.Fatalf("independent primitive result was conflated: %#v", ttlOnly)
	}
	supported := nftSupportFromPrimitiveChecks("nftables v1", nil, nil)
	if !supported.TTLSet || !supported.RateLimit || supported.Reason != "" {
		t.Fatalf("proven primitives were not reported supported: %#v", supported)
	}
	if got := nftCapabilityCheckArguments(); len(got) != 3 || got[0] != "--check" || got[1] != "--file" || got[2] != "-" {
		t.Fatalf("capability probe is not the fixed read-only nft check contract: %#v", got)
	}
}

func TestManagedSemanticIdentityIgnoresVolatileFormattingAndAging(t *testing.T) {
	revision := strings.Repeat("a", 64)
	candidate := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n  set solovey_allow_tcp_ports {\n    type inet_service\n    elements = { 22 }\n  }\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    counter accept\n  }\n}\n")
	live := []byte("table inet solovey_protection { # handle 42\n comment \"solovey-revision:" + revision + "\" # handle 99\n set solovey_allow_tcp_ports { # handle 7\n  type inet_service\n  elements = { 22 }\n }\n chain solovey_input { # handle 8\n  type filter hook input priority -5; policy accept;\n  counter packets 912 bytes 4096 accept\n }\n}\n")
	want, err := ManagedSemanticSHA256(candidate)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := canonicalManagedNFT(live, false)
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(append([]byte(ManagedNFTSemanticSchemaV1+"\x00"), got...)) != want {
		t.Fatalf("volatile nft formatting changed semantic identity: want=%s got=%s", want, sha256Hex(got))
	}
}

func TestManagedSemanticIdentityDetectsRuleDrift(t *testing.T) {
	revision := strings.Repeat("b", 64)
	base := []byte("table inet solovey_protection {\n comment \"solovey-revision:" + revision + "\"\n chain solovey_input {\n  type filter hook input priority -5; policy accept;\n  counter accept\n }\n}\n")
	drift := []byte(strings.Replace(string(base), "counter accept", "counter drop", 1))
	baseSHA, err := ManagedSemanticSHA256(base)
	if err != nil {
		t.Fatal(err)
	}
	driftSHA, err := ManagedSemanticSHA256(drift)
	if err != nil {
		t.Fatal(err)
	}
	if baseSHA == driftSHA {
		t.Fatal("semantic rule drift was not detected")
	}
}

func TestAbsoluteTimeCandidateAndUTCListFormsHaveOneSemanticIdentity(t *testing.T) {
	revision := strings.Repeat("d", 64)
	candidate := []byte("table inet solovey_protection {\n comment \"solovey-revision:" + revision + "\"\n chain solovey_input {\n  type filter hook input priority -5; policy accept;\n  meta nfproto ipv4 ip saddr 198.51.100.0/24 ip daddr 192.0.2.5 meta l4proto tcp tcp dport 443 meta time < 2000000000 counter accept\n  counter accept\n }\n}\n")
	live := []byte(strings.Replace(string(candidate), "meta nfproto ipv4 ip saddr 198.51.100.0/24 ip daddr 192.0.2.5 meta l4proto tcp tcp dport 443 meta time < 2000000000", `ip saddr 198.51.100.0/24 ip daddr 192.0.2.5 tcp dport 443 meta time < "2033-05-18 03:33:20"`, 1))
	candidateSHA, err := ManagedSemanticSHA256(candidate)
	if err != nil {
		t.Fatal(err)
	}
	liveSHA, err := ManagedSemanticSHA256(live)
	if err != nil || liveSHA != candidateSHA {
		t.Fatalf("absolute time semantic identity differs: candidate=%s live=%s err=%v", candidateSHA, liveSHA, err)
	}
	if err := validateCandidate(candidate, revision, sha256Hex(candidate)); err != nil {
		t.Fatalf("generated absolute-time accept was rejected: %v", err)
	}
	drift := []byte(strings.Replace(string(live), "03:33:20", "03:33:21", 1))
	driftSHA, err := ManagedSemanticSHA256(drift)
	if err != nil || driftSHA == candidateSHA {
		t.Fatalf("different absolute expiry was not semantic drift: candidate=%s drift=%s err=%v", candidateSHA, driftSHA, err)
	}
	unsafe := []byte(strings.Replace(string(candidate), "counter accept\n  counter accept", "jump solovey_endpoint_aaaaaaaaaaaa\n  counter accept", 1))
	if err := validateCandidate(unsafe, revision, sha256Hex(unsafe)); err == nil {
		t.Fatal("absolute time was accepted on a generated endpoint jump")
	}
}

func TestManagedSemanticIdentitySeparatesStaticPolicyFromMutableTimedSetState(t *testing.T) {
	revision := strings.Repeat("c", 64)
	configured := []byte("table inet solovey_protection {\n comment \"solovey-revision:" + revision + "\"\n set solovey_block4_aaaaaaaaaaaa {\n  type ipv4_addr\n  flags interval,timeout\n  size 4096\n  timeout 3600s\n  elements = { 192.0.2.1 timeout 300s }\n }\n chain solovey_input {\n  type filter hook input priority -5; policy accept;\n  counter accept\n }\n}\n")
	aged := []byte("table inet solovey_protection { # handle 1\n comment \"solovey-revision:" + revision + "\"\n set solovey_block4_aaaaaaaaaaaa { # handle 2\n  type ipv4_addr\n  size 4096 # count 0\n  flags timeout,interval\n  timeout 1h\n }\n chain solovey_input { # handle 3\n  type filter hook input priority filter - 5; policy accept;\n  counter packets 9 bytes 700 accept\n }\n}\n")
	want, err := ManagedSemanticSHA256(configured)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ManagedSemanticSHA256(aged)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("normal timed-set membership expiry changed static identity: want=%s got=%s", want, got)
	}
	configuredMembership, err := ManagedTimedMembershipSHA256(configured)
	if err != nil {
		t.Fatal(err)
	}
	agedMembership, err := ManagedTimedMembershipSHA256(aged)
	if err != nil {
		t.Fatal(err)
	}
	if configuredMembership == agedMembership {
		t.Fatal("premature timed-set membership loss was normalized away")
	}
	ttlAged := []byte(strings.Replace(string(configured), "timeout 300s", "expires 17s", 1))
	ttlAgedMembership, err := ManagedTimedMembershipSHA256(ttlAged)
	if err != nil || ttlAgedMembership != configuredMembership {
		t.Fatalf("remaining TTL changed timed membership identity: got=%s want=%s err=%v", ttlAgedMembership, configuredMembership, err)
	}
	inserted := []byte(strings.Replace(string(configured), "192.0.2.1 timeout 300s", "192.0.2.1 timeout 300s, 198.51.100.2 expires 30s", 1))
	insertedMembership, err := ManagedTimedMembershipSHA256(inserted)
	if err != nil || insertedMembership == configuredMembership {
		t.Fatalf("unexpected timed-set insertion was not detected: got=%s err=%v", insertedMembership, err)
	}
	for name, mutation := range map[string]string{
		"set type":        strings.Replace(string(configured), "type ipv4_addr", "type ipv6_addr", 1),
		"set flags":       strings.Replace(string(configured), "flags interval,timeout", "flags interval", 1),
		"default timeout": strings.Replace(string(configured), "timeout 3600s", "timeout 7200s", 1),
		"size":            strings.Replace(string(configured), "size 4096", "size 2048", 1),
	} {
		t.Run(name, func(t *testing.T) {
			changed, semanticErr := ManagedSemanticSHA256([]byte(mutation))
			if semanticErr != nil {
				t.Fatal(semanticErr)
			}
			if changed == want {
				t.Fatalf("%s drift was normalized away", name)
			}
		})
	}
}

func TestManagedCandidateSizeContractRejectsBeforeExecution(t *testing.T) {
	revision := strings.Repeat("f", 64)
	base := []byte("table inet solovey_protection {\n comment \"solovey-revision:" + revision + "\"\n chain solovey_input {\n  counter accept\n }\n}\n")
	for _, size := range []int{MaxManagedCandidateBytes - 1, MaxManagedCandidateBytes} {
		candidate := append(append([]byte(nil), base...), make([]byte, size-len(base))...)
		for index := len(base); index < len(candidate); index++ {
			candidate[index] = '\n'
		}
		if err := validateCandidate(candidate, revision, sha256Hex(candidate)); err != nil {
			t.Fatalf("candidate size %d rejected: %v", size, err)
		}
	}
	tooLarge := append(append([]byte(nil), base...), make([]byte, MaxManagedCandidateBytes+1-len(base))...)
	if err := validateCandidate(tooLarge, revision, sha256Hex(tooLarge)); err == nil {
		t.Fatal("candidate above the observable size contract was accepted")
	}
	executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}}
	if executor.checks != 0 || executor.applies != 0 {
		t.Fatal("size rejection mutated the executor")
	}
}

func TestManagedSemanticIdentityDetectsTopologyAndOrderDrift(t *testing.T) {
	revision := strings.Repeat("d", 64)
	base := "table inet solovey_protection {\n comment \"solovey-revision:" + revision + "\"\n set solovey_allow_tcp_ports {\n  type inet_service\n  flags interval\n  elements = { 22, 443 }\n }\n chain solovey_input {\n  type filter hook input priority -5; policy accept;\n  iifname \"lo\" counter accept\n  counter accept\n }\n}\n"
	baseSHA, err := ManagedSemanticSHA256([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]string{
		"expression":    strings.Replace(base, `iifname "lo"`, `iifname "eth0"`, 1),
		"verdict":       strings.Replace(base, "counter accept", "counter drop", 1),
		"rule order":    strings.Replace(base, "  iifname \"lo\" counter accept\n  counter accept", "  counter accept\n  iifname \"lo\" counter accept", 1),
		"hook priority": strings.Replace(base, "priority -5", "priority 0", 1),
		"policy":        strings.Replace(base, "policy accept", "policy drop", 1),
		"foreign rule":  strings.Replace(base, "  counter accept\n }", "  counter accept\n  meta mark 1 counter drop\n }", 1),
		"foreign chain": strings.TrimSuffix(base, "}\n") + " chain foreign_extra {\n  counter accept\n }\n}\n",
		"foreign set":   strings.TrimSuffix(base, "}\n") + " set foreign_extra {\n  type ipv4_addr\n  elements = { 192.0.2.1 }\n }\n}\n",
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			changed, semanticErr := ManagedSemanticSHA256([]byte(mutation))
			if semanticErr != nil {
				t.Fatal(semanticErr)
			}
			if changed == baseSHA {
				t.Fatalf("%s drift was normalized away", name)
			}
		})
	}
	duplicate := strings.TrimSuffix(base, "}\n") + " chain solovey_input {\n  counter accept\n }\n}\n"
	if _, err := ManagedSemanticSHA256([]byte(duplicate)); err == nil {
		t.Fatal("duplicate managed object was accepted")
	}
}

type fakeNFTExecutor struct {
	support          NFTSupport
	state            []byte
	present          bool
	checkErr         error
	listErr          error
	applyErr         error
	deleteErr        error
	deleteCalls      int
	verifyMismatch   bool
	rollbackMismatch bool
	checks           int
	applies          int
}

func testSemanticSHA(t *testing.T, data []byte) string {
	t.Helper()
	value, err := ManagedSemanticSHA256(data)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testTimedMembershipSHA(t *testing.T, data []byte) string {
	t.Helper()
	value, err := ManagedTimedMembershipSHA256(data)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func (f *fakeNFTExecutor) Detect(context.Context) NFTSupport { return f.support }
func (f *fakeNFTExecutor) CheckManagedFile(context.Context, string) error {
	f.checks++
	return f.checkErr
}
func (f *fakeNFTExecutor) ListManagedTable(context.Context) ([]byte, bool, error) {
	if f.listErr != nil {
		return nil, false, f.listErr
	}
	return append([]byte(nil), f.state...), f.present, nil
}
func (f *fakeNFTExecutor) ApplyManagedFile(_ context.Context, path string) error {
	f.applies++
	if f.applyErr != nil {
		return f.applyErr
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	index := strings.LastIndex(text, "table inet solovey_protection {")
	if index < 0 {
		f.state, f.present = nil, false
		return nil
	}
	f.state, f.present = append([]byte(nil), data[index:]...), true
	if f.verifyMismatch {
		f.state = []byte(strings.Replace(string(f.state), "\"\n", "-forged\"\n", 1))
	}
	if f.rollbackMismatch && f.applies > 1 {
		f.state = []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + strings.Repeat("f", 64) + "\"\n}\n")
	}
	return nil
}

func (f *fakeNFTExecutor) DeleteManagedTable(context.Context) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.state, f.present = nil, false
	return nil
}

func TestPackageRemovalDeletesOnlyAuthenticatedManagedTableAndIsIdempotent(t *testing.T) {
	revision := strings.Repeat("9", 64)
	managed := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n  chain solovey_input {\n    type filter hook input priority -5; policy accept;\n    counter accept\n  }\n}\n")
	t.Run("present then absent", func(t *testing.T) {
		executor := &fakeNFTExecutor{state: managed, present: true}
		if err := removeManagedTableForPackageRemoval(t.Context(), executor); err != nil {
			t.Fatal(err)
		}
		if executor.present || executor.deleteCalls != 1 {
			t.Fatalf("managed cleanup result = present:%v deletes:%d", executor.present, executor.deleteCalls)
		}
		if err := removeManagedTableForPackageRemoval(t.Context(), executor); err != nil || executor.deleteCalls != 1 {
			t.Fatalf("idempotent absent cleanup = deletes:%d err:%v", executor.deleteCalls, err)
		}
	})
	t.Run("foreign same-name table", func(t *testing.T) {
		executor := &fakeNFTExecutor{state: []byte("table inet solovey_protection {\n  chain foreign {\n    counter accept\n  }\n}\n"), present: true}
		if err := removeManagedTableForPackageRemoval(t.Context(), executor); err == nil || executor.deleteCalls != 0 || !executor.present {
			t.Fatalf("foreign table cleanup = present:%v deletes:%d err:%v", executor.present, executor.deleteCalls, err)
		}
	})
	t.Run("delete failure", func(t *testing.T) {
		sentinel := errors.New("injected delete failure")
		executor := &fakeNFTExecutor{state: managed, present: true, deleteErr: sentinel}
		if err := removeManagedTableForPackageRemoval(t.Context(), executor); !errors.Is(err, sentinel) || executor.deleteCalls != 1 || !executor.present {
			t.Fatalf("failed cleanup = present:%v deletes:%d err:%v", executor.present, executor.deleteCalls, err)
		}
	})
}

func TestNFTBackendRollbackMustRestoreRecordedManagedRevision(t *testing.T) {
	root := testManagedRoot(t)
	revision := strings.Repeat("1", 64)
	previousRevision := strings.Repeat("0", 64)
	candidate := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n}\n")
	previous := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + previousRevision + "\"\n}\n")
	writeTestManagedFile(t, root, "revisions/rollback-verify/candidate.nft", candidate)
	executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}, state: previous, present: true, rollbackMismatch: true}
	engine := newContractEngineWithExecutor(root, executor)
	correlation := Correlation{OperationID: "operation-rollback-verify", InstanceID: "instance-1", LockRevision: 7}
	apply := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{
		CandidatePath: "revisions/rollback-verify/candidate.nft", RollbackArtifactPath: "revisions/rollback-verify/firewall-before.nft", ExpectedTable: managedTable,
		ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: testSemanticSHA(t, candidate), ExpectedTimedMembershipSHA256: testTimedMembershipSHA(t, candidate), ExpectedPreviousRevision: previousRevision, ExpectedPreviousSemanticSHA256: testSemanticSHA(t, previous), ExpectedPreviousTimedMembershipSHA256: testTimedMembershipSHA(t, previous), ExpectedPreviousTablePresent: true,
	}})
	if !apply.OK {
		t.Fatalf("setup apply failed: %#v", apply)
	}
	rollback := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{
		RollbackArtifactPath: "revisions/rollback-verify/firewall-before.nft", ExpectedTable: managedTable, ExpectedCurrentRevision: revision, ExpectedCurrentSemanticSHA256: testSemanticSHA(t, candidate), ExpectedCurrentTimedMembershipSHA256: testTimedMembershipSHA(t, candidate),
	}})
	if rollback.OK || rollback.Code != CodeValidationFailed {
		t.Fatalf("rollback revision mismatch reported success: %#v", rollback)
	}
}

func TestNFTBackendRefusesToReplaceUnversionedManagedTable(t *testing.T) {
	root := testManagedRoot(t)
	revision := strings.Repeat("2", 64)
	candidate := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n}\n")
	writeTestManagedFile(t, root, "revisions/unversioned/candidate.nft", candidate)
	executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}, state: []byte("table inet solovey_protection {\n}\n"), present: true}
	response := newContractEngineWithExecutor(root, executor).Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-unversioned", InstanceID: "instance-1", LockRevision: 1}, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{
		CandidatePath: "revisions/unversioned/candidate.nft", RollbackArtifactPath: "revisions/unversioned/firewall-before.nft", ExpectedTable: managedTable,
		ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: testSemanticSHA(t, candidate), ExpectedTimedMembershipSHA256: testTimedMembershipSHA(t, candidate), ExpectedPreviousRevision: strings.Repeat("0", 64), ExpectedPreviousSemanticSHA256: strings.Repeat("f", 64), ExpectedPreviousTimedMembershipSHA256: strings.Repeat("e", 64), ExpectedPreviousTablePresent: true,
	}})
	if response.OK || executor.applies != 0 {
		t.Fatalf("unversioned managed state was replaced: response=%#v applies=%d", response, executor.applies)
	}
}

func TestNFTBackendApplyVerifyAndRollbackAreManagedOnly(t *testing.T) {
	root := testManagedRoot(t)
	revision := strings.Repeat("1", 64)
	candidate := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n}\n")
	writeTestManagedFile(t, root, "revisions/op/candidate.nft", candidate)
	previous := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + strings.Repeat("0", 64) + "\"\n}\n")
	executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}, state: previous, present: true}
	engine := newContractEngineWithExecutor(root, executor)
	correlation := Correlation{OperationID: "operation-1", InstanceID: "instance-1", LockRevision: 7}
	apply := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{
		CandidatePath: "revisions/op/candidate.nft", RollbackArtifactPath: "revisions/op/firewall-before.nft", ExpectedTable: managedTable,
		ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: testSemanticSHA(t, candidate), ExpectedTimedMembershipSHA256: testTimedMembershipSHA(t, candidate), ExpectedPreviousRevision: strings.Repeat("0", 64), ExpectedPreviousSemanticSHA256: testSemanticSHA(t, previous), ExpectedPreviousTimedMembershipSHA256: testTimedMembershipSHA(t, previous), ExpectedPreviousTablePresent: true,
	}})
	if !apply.OK || apply.NFT == nil || apply.NFT.AppliedRevision != revision || apply.NFT.RollbackSHA256 != sha256Hex(previous) {
		t.Fatalf("apply response = %#v", apply)
	}
	rollback := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{
		RollbackArtifactPath: "revisions/op/firewall-before.nft", ExpectedTable: managedTable, ExpectedCurrentRevision: revision, ExpectedCurrentSemanticSHA256: testSemanticSHA(t, candidate), ExpectedCurrentTimedMembershipSHA256: testTimedMembershipSHA(t, candidate),
	}})
	if !rollback.OK || rollback.NFT == nil || string(executor.state) != string(previous) {
		t.Fatalf("rollback response=%#v state=%q", rollback, executor.state)
	}
	if executor.applies != 2 || executor.checks != 2 {
		t.Fatalf("restricted executor calls: apply=%d check=%d", executor.applies, executor.checks)
	}
	replayed := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{RollbackArtifactPath: "revisions/op/firewall-before.nft", ExpectedTable: managedTable, ExpectedCurrentRevision: revision, ExpectedCurrentSemanticSHA256: testSemanticSHA(t, candidate), ExpectedCurrentTimedMembershipSHA256: testTimedMembershipSHA(t, candidate)}})
	if !replayed.OK || replayed.NFT == nil || replayed.NFT.CurrentSemanticSHA256 != testSemanticSHA(t, previous) || executor.applies != 2 || executor.checks != 2 {
		t.Fatalf("idempotent rollback replay mutated state: response=%#v applies=%d checks=%d", replayed, executor.applies, executor.checks)
	}
}

func TestNFTBackendRollbackPreservesAbsoluteTimedMemberDeadline(t *testing.T) {
	for _, test := range []struct {
		name          string
		rollbackAfter time.Duration
		wantMember    bool
		wantRemaining string
	}{
		{name: "before expiry", rollbackAfter: 7 * time.Second, wantMember: true, wantRemaining: "expires 3000ms"},
		{name: "exact expiry", rollbackAfter: 10 * time.Second},
		{name: "after expiry", rollbackAfter: 11 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testManagedRoot(t)
			revisionA, revisionB := strings.Repeat("a", 64), strings.Repeat("b", 64)
			previous := []byte("table inet solovey_protection {\n comment \"solovey-revision:" + revisionA + "\"\n set solovey_block4_aaaaaaaaaaaa {\n  type ipv4_addr\n  flags interval,timeout\n  size 4096\n  timeout 60s\n  elements = { 192.0.2.1 timeout 10s expires 10s }\n }\n chain solovey_input {\n  type filter hook input priority -5; policy accept;\n  counter accept\n }\n}\n")
			candidate := []byte("table inet solovey_protection {\n comment \"solovey-revision:" + revisionB + "\"\n chain solovey_input {\n  type filter hook input priority -5; policy accept;\n  counter accept\n }\n}\n")
			writeTestManagedFile(t, root, "revisions/absolute-deadline/candidate.nft", candidate)
			executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}, state: previous, present: true}
			clock := time.Unix(10_000, 0).UTC()
			engine := newContractEngineWithExecutor(root, executor)
			engine.now = func() time.Time { return clock }
			correlation := Correlation{OperationID: "operation-absolute-deadline", InstanceID: "instance", LockRevision: 1}
			applied := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{
				CandidatePath: "revisions/absolute-deadline/candidate.nft", RollbackArtifactPath: "revisions/absolute-deadline/firewall-before.nft", ExpectedTable: managedTable,
				ExpectedRevision: revisionB, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: testSemanticSHA(t, candidate), ExpectedTimedMembershipSHA256: testTimedMembershipSHA(t, candidate),
				ExpectedPreviousRevision: revisionA, ExpectedPreviousSemanticSHA256: testSemanticSHA(t, previous), ExpectedPreviousTimedMembershipSHA256: testTimedMembershipSHA(t, previous), ExpectedPreviousTablePresent: true,
			}})
			if !applied.OK || applied.NFT == nil {
				t.Fatalf("setup apply failed: %#v", applied)
			}
			artifactPath, err := root.Resolve("revisions/absolute-deadline/firewall-before.nft", true)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := os.ReadFile(artifactPath)
			if err != nil || !strings.Contains(string(artifact), managedNFTRollbackArtifactSchemaV1) || strings.Contains(string(artifact), "expires 10s") {
				t.Fatalf("rollback deadline was not sealed absolutely: artifact=%q err=%v", artifact, err)
			}
			clock = clock.Add(test.rollbackAfter)
			rolled := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{
				RollbackArtifactPath: "revisions/absolute-deadline/firewall-before.nft", ExpectedTable: managedTable, ExpectedSHA256: applied.NFT.RollbackSHA256,
				ExpectedCurrentRevision: revisionB, ExpectedCurrentSemanticSHA256: testSemanticSHA(t, candidate), ExpectedCurrentTimedMembershipSHA256: testTimedMembershipSHA(t, candidate),
			}})
			if !rolled.OK || rolled.NFT == nil || !rolled.NFT.ManagedTablePresent {
				t.Fatalf("deadline-aware rollback failed: %#v", rolled)
			}
			hasMember := strings.Contains(string(executor.state), "192.0.2.1")
			if hasMember != test.wantMember || test.wantRemaining != "" && !strings.Contains(string(executor.state), test.wantRemaining) || strings.Contains(string(executor.state), "expires 10s") {
				t.Fatalf("restored timed state=%q wantMember=%v wantRemaining=%q", executor.state, test.wantMember, test.wantRemaining)
			}
		})
	}
}

func TestNFTBackendRollbackRefusesUnexpectedCurrentRevision(t *testing.T) {
	root := testManagedRoot(t)
	revision := strings.Repeat("4", 64)
	previousRevision := strings.Repeat("3", 64)
	foreignRevision := strings.Repeat("2", 64)
	candidate := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n}\n")
	previous := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + previousRevision + "\"\n}\n")
	writeTestManagedFile(t, root, "revisions/current-fence/candidate.nft", candidate)
	executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}, state: previous, present: true}
	engine := newContractEngineWithExecutor(root, executor)
	correlation := Correlation{OperationID: "operation-current-fence", InstanceID: "instance-1", LockRevision: 2}
	apply := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{CandidatePath: "revisions/current-fence/candidate.nft", RollbackArtifactPath: "revisions/current-fence/firewall-before.nft", ExpectedTable: managedTable, ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: testSemanticSHA(t, candidate), ExpectedTimedMembershipSHA256: testTimedMembershipSHA(t, candidate), ExpectedPreviousRevision: previousRevision, ExpectedPreviousSemanticSHA256: testSemanticSHA(t, previous), ExpectedPreviousTimedMembershipSHA256: testTimedMembershipSHA(t, previous), ExpectedPreviousTablePresent: true}})
	if !apply.OK {
		t.Fatalf("setup apply failed: %#v", apply)
	}
	executor.state = []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + foreignRevision + "\"\n}\n")
	rollback := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{RollbackArtifactPath: "revisions/current-fence/firewall-before.nft", ExpectedTable: managedTable, ExpectedCurrentRevision: revision, ExpectedCurrentSemanticSHA256: testSemanticSHA(t, candidate), ExpectedCurrentTimedMembershipSHA256: testTimedMembershipSHA(t, candidate)}})
	if rollback.OK || executor.applies != 1 || !strings.Contains(string(executor.state), foreignRevision) {
		t.Fatalf("rollback replaced an unexpected current revision: response=%#v applies=%d state=%q", rollback, executor.applies, executor.state)
	}
}

func TestNFTBackendApplyRefusesManagedTableDriftAfterValidation(t *testing.T) {
	root := testManagedRoot(t)
	revision := strings.Repeat("6", 64)
	previousRevision := strings.Repeat("5", 64)
	candidate := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n}\n")
	previous := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + previousRevision + "\"\n}\n")
	writeTestManagedFile(t, root, "revisions/apply-fence/candidate.nft", candidate)
	executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}, state: previous, present: true}
	engine := newContractEngineWithExecutor(root, executor)
	correlation := Correlation{OperationID: "operation-apply-fence", InstanceID: "instance-1", LockRevision: 2}
	validated := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTValidate, NFTValidate: &NFTValidateRequest{CandidatePath: "revisions/apply-fence/candidate.nft", ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: testSemanticSHA(t, candidate), ExpectedTimedMembershipSHA256: testTimedMembershipSHA(t, candidate)}})
	if !validated.OK || validated.NFT == nil || validated.NFT.PreviousRevision != previousRevision || validated.NFT.PreviousSemanticSHA256 != testSemanticSHA(t, previous) || !validated.NFT.PreviousTablePresent {
		t.Fatalf("validation did not return an exact current-table fence: %#v", validated)
	}
	executor.state = []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + previousRevision + "\"\n\n}\n")
	apply := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{CandidatePath: "revisions/apply-fence/candidate.nft", RollbackArtifactPath: "revisions/apply-fence/firewall-before.nft", ExpectedTable: managedTable, ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: testSemanticSHA(t, candidate), ExpectedTimedMembershipSHA256: testTimedMembershipSHA(t, candidate), ExpectedPreviousRevision: validated.NFT.PreviousRevision, ExpectedPreviousSemanticSHA256: validated.NFT.PreviousSemanticSHA256, ExpectedPreviousTimedMembershipSHA256: validated.NFT.PreviousTimedMembershipSHA256, ExpectedPreviousTablePresent: true}})
	if !apply.OK || executor.applies != 1 || !strings.Contains(string(executor.state), revision) {
		t.Fatalf("formatting-only live state was not treated as semantic match: response=%#v applies=%d state=%q", apply, executor.applies, executor.state)
	}
}

func TestNFTBackendRejectsUnmanagedMutationBeforeExecutor(t *testing.T) {
	root := testManagedRoot(t)
	revision := strings.Repeat("2", 64)
	for name, candidate := range map[string]string{
		"unmanaged":  "table inet user_rules {\n}\n",
		"full-flush": "flush ruleset\ntable inet solovey_protection {\n}\n",
		"include":    "include \"/tmp/rules\"\ntable inet solovey_protection {\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte(strings.Replace(candidate, "{\n", "{\n  comment \"solovey-revision:"+revision+"\"\n", 1))
			path := "revisions/" + name + "/candidate.nft"
			writeTestManagedFile(t, root, path, data)
			executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}}
			response := newContractEngineWithExecutor(root, executor).Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-2", InstanceID: "instance-2", LockRevision: 1}, Operation: OperationNFTValidate, NFTValidate: &NFTValidateRequest{CandidatePath: path, ExpectedRevision: revision, ExpectedSHA256: sha256Hex(data), ExpectedSemanticSHA256: strings.Repeat("f", 64), ExpectedTimedMembershipSHA256: strings.Repeat("e", 64)}})
			if response.OK || executor.checks != 0 || executor.applies != 0 {
				t.Fatalf("unsafe artifact reached executor: %#v", response)
			}
		})
	}
}

func TestRollbackScopeRejectsAdditionalTopLevelMutation(t *testing.T) {
	artifact := []byte("delete table inet solovey_protection\nadd chain inet user_rules injected\n")
	if err := validateManagedScope(artifact, true); err == nil {
		t.Fatal("rollback artifact accepted an unmanaged top-level mutation")
	}
}

func TestNFTCapabilityUnknownAndFailureInjectionNeverReportSuccess(t *testing.T) {
	root := testManagedRoot(t)
	unknown := newContractEngineWithExecutor(root, nil).Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "capabilities", InstanceID: "instance"}, Operation: OperationCapabilities, Capabilities: &CapabilitiesRequest{}})
	if !unknown.OK || unknown.Capabilities.NFT.PlatformKnown || CapabilityAvailable(unknown.Capabilities, OperationNFTApply) {
		t.Fatalf("unknown capability became supported: %#v", unknown)
	}
	revision := strings.Repeat("3", 64)
	candidate := []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + revision + "\"\n}\n")
	writeTestManagedFile(t, root, "revisions/fail/candidate.nft", candidate)
	for name, configure := range map[string]func(*fakeNFTExecutor){
		"check":  func(f *fakeNFTExecutor) { f.checkErr = errors.New("injected check failure") },
		"apply":  func(f *fakeNFTExecutor) { f.applyErr = errors.New("injected apply failure") },
		"verify": func(f *fakeNFTExecutor) { f.verifyMismatch = true },
	} {
		t.Run(name, func(t *testing.T) {
			executor := &fakeNFTExecutor{support: NFTSupport{PlatformKnown: true, Linux: true, Available: true}}
			configure(executor)
			response := newContractEngineWithExecutor(root, executor).Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-fail-" + name, InstanceID: "instance", LockRevision: 1}, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{CandidatePath: "revisions/fail/candidate.nft", RollbackArtifactPath: "revisions/fail/" + name + "-before.nft", ExpectedTable: managedTable, ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedSemanticSHA256: testSemanticSHA(t, candidate), ExpectedTimedMembershipSHA256: testTimedMembershipSHA(t, candidate)}})
			if response.OK || response.Code != CodeValidationFailed {
				t.Fatalf("failure injection reported success: %#v", response)
			}
		})
	}
}

func TestValidateCandidateAcceptsOnlyTypedEndpointManagedGrammar(t *testing.T) {
	revision := strings.Repeat("a", 64)
	candidate := []byte("table inet solovey_protection {\n" +
		"  comment \"solovey-revision:" + revision + "\"\n" +
		"  set solovey_block4_bbbbbbbbbbbb {\n" +
		"    type ipv4_addr\n" +
		"    flags interval,timeout\n" +
		"    size 4096\n" +
		"    timeout 14400s\n" +
		"    elements = { 203.0.113.10/32 timeout 600s }\n" +
		"  }\n" +
		"  chain solovey_input {\n" +
		"    type filter hook input priority -5; policy accept;\n" +
		"    meta nfproto ipv4 ip saddr 198.51.100.0/24 ip daddr 192.0.2.5 meta l4proto tcp tcp dport 443 counter accept\n" +
		"    iifname \"lo\" counter accept\n" +
		"    ct state established,related counter accept\n" +
		"    meta nfproto ipv4 ip daddr 192.0.2.5 meta l4proto tcp tcp dport 443 jump solovey_endpoint_bbbbbbbbbbbb\n" +
		"  }\n" +
		"  chain solovey_endpoint_bbbbbbbbbbbb {\n" +
		"    ip saddr @solovey_block4_bbbbbbbbbbbb counter drop\n" +
		"    counter accept\n" +
		"  }\n" +
		"}\n")
	if err := validateCandidate(candidate, revision, sha256Hex(candidate)); err != nil {
		t.Fatalf("typed endpoint candidate was rejected: %v\n%s", err, candidate)
	}
	for name, mutation := range map[string][]byte{
		"unmanaged-table":   []byte(strings.Replace(string(candidate), "table inet solovey_protection", "table inet user_rules", 1)),
		"raw-include":       []byte(strings.Replace(string(candidate), "  chain solovey_input", "  include \"/tmp/raw.nft\"\n  chain solovey_input", 1)),
		"arbitrary-rule":    []byte(strings.Replace(string(candidate), "    counter accept", "    meta mark set 1 counter accept", 1)),
		"protocol-mismatch": []byte(strings.Replace(string(candidate), "meta l4proto tcp tcp dport", "meta l4proto tcp udp dport", 1)),
		"family-mismatch":   []byte(strings.Replace(string(candidate), "meta nfproto ipv4 ip saddr", "meta nfproto ipv4 ip6 saddr", 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateCandidate(mutation, revision, sha256Hex(mutation)); err == nil {
				t.Fatal("candidate accepted a non-generated mutation")
			}
		})
	}
}

func writeTestManagedFile(t *testing.T, root ManagedRoot, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root.Path(), filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
