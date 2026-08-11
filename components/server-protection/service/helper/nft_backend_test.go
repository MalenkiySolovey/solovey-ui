package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

type fakeNFTExecutor struct {
	support          NFTSupport
	state            []byte
	present          bool
	checkErr         error
	listErr          error
	applyErr         error
	verifyMismatch   bool
	rollbackMismatch bool
	checks           int
	applies          int
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
		ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedPreviousRevision: previousRevision, ExpectedPreviousSHA256: sha256Hex(previous), ExpectedPreviousTablePresent: true,
	}})
	if !apply.OK {
		t.Fatalf("setup apply failed: %#v", apply)
	}
	rollback := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{
		RollbackArtifactPath: "revisions/rollback-verify/firewall-before.nft", ExpectedTable: managedTable, ExpectedCurrentRevision: revision,
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
		ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedPreviousRevision: strings.Repeat("0", 64), ExpectedPreviousSHA256: sha256Hex(executor.state), ExpectedPreviousTablePresent: true,
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
		ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedPreviousRevision: strings.Repeat("0", 64), ExpectedPreviousSHA256: sha256Hex(previous), ExpectedPreviousTablePresent: true,
	}})
	if !apply.OK || apply.NFT == nil || apply.NFT.AppliedRevision != revision || apply.NFT.RollbackSHA256 != sha256Hex(previous) {
		t.Fatalf("apply response = %#v", apply)
	}
	rollback := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{
		RollbackArtifactPath: "revisions/op/firewall-before.nft", ExpectedTable: managedTable, ExpectedCurrentRevision: revision,
	}})
	if !rollback.OK || rollback.NFT == nil || string(executor.state) != string(previous) {
		t.Fatalf("rollback response=%#v state=%q", rollback, executor.state)
	}
	if executor.applies != 2 || executor.checks != 2 {
		t.Fatalf("restricted executor calls: apply=%d check=%d", executor.applies, executor.checks)
	}
	replayed := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{RollbackArtifactPath: "revisions/op/firewall-before.nft", ExpectedTable: managedTable, ExpectedCurrentRevision: revision}})
	if !replayed.OK || replayed.NFT == nil || executor.applies != 2 || executor.checks != 2 {
		t.Fatalf("idempotent rollback replay mutated state: response=%#v applies=%d checks=%d", replayed, executor.applies, executor.checks)
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
	apply := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{CandidatePath: "revisions/current-fence/candidate.nft", RollbackArtifactPath: "revisions/current-fence/firewall-before.nft", ExpectedTable: managedTable, ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedPreviousRevision: previousRevision, ExpectedPreviousSHA256: sha256Hex(previous), ExpectedPreviousTablePresent: true}})
	if !apply.OK {
		t.Fatalf("setup apply failed: %#v", apply)
	}
	executor.state = []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + foreignRevision + "\"\n}\n")
	rollback := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTRollback, NFTRollback: &NFTRollbackRequest{RollbackArtifactPath: "revisions/current-fence/firewall-before.nft", ExpectedTable: managedTable, ExpectedCurrentRevision: revision}})
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
	validated := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTValidate, NFTValidate: &NFTValidateRequest{CandidatePath: "revisions/apply-fence/candidate.nft", ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate)}})
	if !validated.OK || validated.NFT == nil || validated.NFT.PreviousRevision != previousRevision || validated.NFT.PreviousSHA256 != sha256Hex(previous) || !validated.NFT.PreviousTablePresent {
		t.Fatalf("validation did not return an exact current-table fence: %#v", validated)
	}
	executor.state = []byte("table inet solovey_protection {\n  comment \"solovey-revision:" + previousRevision + "\"\n\n}\n")
	apply := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{CandidatePath: "revisions/apply-fence/candidate.nft", RollbackArtifactPath: "revisions/apply-fence/firewall-before.nft", ExpectedTable: managedTable, ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate), ExpectedPreviousRevision: validated.NFT.PreviousRevision, ExpectedPreviousSHA256: validated.NFT.PreviousSHA256, ExpectedPreviousTablePresent: true}})
	if apply.OK || executor.applies != 0 || !strings.Contains(string(executor.state), previousRevision) {
		t.Fatalf("apply replaced managed state that drifted after validation: response=%#v applies=%d state=%q", apply, executor.applies, executor.state)
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
			response := newContractEngineWithExecutor(root, executor).Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-2", InstanceID: "instance-2", LockRevision: 1}, Operation: OperationNFTValidate, NFTValidate: &NFTValidateRequest{CandidatePath: path, ExpectedRevision: revision, ExpectedSHA256: sha256Hex(data)}})
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
			response := newContractEngineWithExecutor(root, executor).Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-fail-" + name, InstanceID: "instance", LockRevision: 1}, Operation: OperationNFTApply, NFTApply: &NFTApplyRequest{CandidatePath: "revisions/fail/candidate.nft", RollbackArtifactPath: "revisions/fail/" + name + "-before.nft", ExpectedTable: managedTable, ExpectedRevision: revision, ExpectedSHA256: sha256Hex(candidate)}})
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
