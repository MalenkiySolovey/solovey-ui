package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

func TestArbitraryOperationRejected(t *testing.T) {
	root := testManagedRoot(t)
	request := Request{
		ProtocolVersion: ProtocolVersion,
		Correlation:     Correlation{OperationID: "op-1", InstanceID: "instance-1", LockRevision: 1},
		Operation:       Operation("shell.execute"),
		Capabilities:    &CapabilitiesRequest{},
	}
	if err := request.Validate(root); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("arbitrary operation was not rejected: %v", err)
	}
}

func TestUnknownArgumentRejected(t *testing.T) {
	payload := `{"protocol_version":1,"correlation":{"operation_id":"op-1","instance_id":"instance-1","lock_revision":1},"operation":"nginx.reload","nginx_reload":{"config_path":"nginx/generated.conf","raw_flags":["-s","reload"]}}`
	if _, err := DecodeRequest(strings.NewReader(payload)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown argument was not rejected: %v", err)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	root := testManagedRoot(t)
	if _, err := root.Resolve(`..\outside`, false); err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("path traversal was not rejected: %v", err)
	}
}

func TestSymlinkEscapeRejectedByResolvedPathContract(t *testing.T) {
	root := testManagedRoot(t)
	inside := filepath.Join(root.Path(), "escape")
	outside := filepath.Join(filepath.Dir(filepath.Dir(root.Path())), "outside")
	eval := func(path string) (string, error) {
		if filepath.Clean(path) == filepath.Clean(inside) {
			return outside, nil
		}
		return filepath.EvalSymlinks(path)
	}
	if _, err := root.resolve("escape", true, eval); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("resolved symlink escape was not rejected: %v", err)
	}
}

func TestManagedRootSymlinkEscapeRejected(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(base, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(runtimeDir, "server-protection")
	if err := os.Symlink(outside, rootPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation is unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := NewManagedRoot(testRuntimeAuthorityForRoot(t, rootPath)); !errors.Is(err, ErrManagedPathForbidden) {
		t.Fatalf("managed-root symlink escape was not rejected: %v", err)
	}
}

func TestResolvedManagedRootShapeRejectsEscape(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), ".runtime", "server-protection")
	authority := testRuntimeAuthorityForRoot(t, rootPath)
	if err := validateResolvedManagedRoot(authority, filepath.Join(t.TempDir(), "outside")); !errors.Is(err, ErrManagedPathForbidden) {
		t.Fatalf("resolved managed-root escape was accepted: %v", err)
	}
	if err := validateResolvedManagedRoot(authority, rootPath); err != nil {
		t.Fatalf("canonical managed-root shape was rejected: %v", err)
	}
}

func TestOpenWrtInstalledAuthorityIsAcceptedAsExactManagedRoot(t *testing.T) {
	authority := testOpenWrtRuntimeAuthority(t)
	root, err := newManagedRoot(authority,
		func(path string) (os.FileInfo, error) {
			if path != protectionruntime.OpenWrtRuntimeRoot {
				t.Fatalf("helper inspected root %q", path)
			}
			return managedRootDirectoryInfo{}, nil
		},
		func(path string) (string, error) { return path, nil },
		func() error { return nil },
	)
	if err != nil || root.Path() != authority.Path() {
		t.Fatalf("OpenWrt helper managed root = %q, authority = %#v, err = %v", root.Path(), authority, err)
	}
}

func TestOversizedOutputIsTruncatedWithoutShortWrite(t *testing.T) {
	buffer := &boundedBuffer{limit: 8}
	value := bytes.Repeat([]byte("x"), 32)
	written, err := buffer.Write(value)
	if err != nil || written != len(value) {
		t.Fatalf("bounded writer returned a short write: written=%d err=%v", written, err)
	}
	if buffer.buffer.Len() != 8 || !buffer.truncated {
		t.Fatalf("bounded output was not truncated: len=%d truncated=%v", buffer.buffer.Len(), buffer.truncated)
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	root := testManagedRoot(t)
	invoker := NewMockInvoker(availableCapabilities(OperationNginxDetectVersion))
	invoker.Block = true
	client, err := NewClient(root, allowLock{}, invoker, &auditCapture{})
	if err != nil {
		t.Fatal(err)
	}
	request := nginxDetectRequest()

	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer timeoutCancel()
	response, err := client.Execute(timeoutCtx, request)
	if !errors.Is(err, context.DeadlineExceeded) || response.Code != CodeTimeout {
		t.Fatalf("timeout mapping mismatch: response=%+v err=%v", response, err)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err = client.Execute(cancelCtx, request)
	if !errors.Is(err, context.Canceled) || response.Code != CodeCanceled {
		t.Fatalf("cancel mapping mismatch: response=%+v err=%v", response, err)
	}
}

func TestVersionMismatchMapsToMissingCapability(t *testing.T) {
	root := testManagedRoot(t)
	capabilities := availableCapabilities(OperationNginxDetectVersion)
	capabilities.HelperVersion = "9.9.9"
	client, err := NewClient(root, allowLock{}, NewMockInvoker(capabilities), &auditCapture{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Execute(context.Background(), nginxDetectRequest())
	if err == nil || response.Code != CodeMissingCapability || response.Reason != "helper_version_mismatch" {
		t.Fatalf("version mismatch mapping mismatch: response=%+v err=%v", response, err)
	}
}

func TestMissingCapabilityRejectedBeforeOperationInvocation(t *testing.T) {
	root := testManagedRoot(t)
	invoker := NewMockInvoker(nil)
	client, err := NewClient(root, allowLock{}, invoker, &auditCapture{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Execute(context.Background(), nginxDetectRequest())
	if err != nil || response.Code != CodeMissingCapability {
		t.Fatalf("missing capability mapping mismatch: response=%+v err=%v", response, err)
	}
	if len(invoker.Requests) != 1 || invoker.Requests[0].Operation != OperationCapabilities {
		t.Fatalf("unavailable operation reached invoker: %#v", invoker.Requests)
	}
}

func TestTerminalReadLockIsLimitedToExactNginxVerify(t *testing.T) {
	root := testManagedRoot(t)
	locks := &splitLockValidator{strictErr: errors.New("strict lock denied"), readErr: errors.New("read lock denied")}
	client, err := NewClient(root, locks, NewMockInvoker(nil), &auditCapture{})
	if err != nil {
		t.Fatal(err)
	}
	revision, sha := strings.Repeat("a", 64), strings.Repeat("b", 64)
	correlation := Correlation{OperationID: "op-terminal-read", InstanceID: "instance-terminal-read", LockRevision: 7}
	verify := Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxVerify, NginxVerify: &NginxVerifyRequest{
		ExpectedRevision: revision, ExpectedSHA256: sha,
		ExpectedBinary: BinaryIdentity{Path: "/usr/sbin/nginx", TargetPath: "/usr/sbin/nginx", Device: 1, Inode: 2,
			Size: 1024, Mode: 0o755, Digest: strings.Repeat("a", 64)},
		Listeners: []NginxListener{{Address: "0.0.0.0", Port: 8443}},
	}}
	if _, err := client.Execute(context.Background(), verify); !errors.Is(err, locks.readErr) || locks.readCalls != 1 || locks.strictCalls != 0 {
		t.Fatalf("verify authorization calls read=%d strict=%d err=%v", locks.readCalls, locks.strictCalls, err)
	}
	switchRequest := Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxSwitch, NginxSwitch: &NginxSwitchRequest{
		ExpectedPreviousRevision: strings.Repeat("c", 64), TargetRevision: revision, ExpectedSHA256: sha,
	}}
	if _, err := client.Execute(context.Background(), switchRequest); !errors.Is(err, locks.strictErr) || locks.readCalls != 1 || locks.strictCalls != 1 {
		t.Fatalf("switch authorization calls read=%d strict=%d err=%v", locks.readCalls, locks.strictCalls, err)
	}
}

func TestExecutionMetadataBindsPinnedHelperAndNegotiatedCapability(t *testing.T) {
	root := testManagedRoot(t)
	capabilities := availableCapabilities(OperationNginxDetectVersion)
	invoker := NewMockInvoker(capabilities)
	invoker.Responses[OperationNginxDetectVersion] = Response{OK: true, NginxVersion: &NginxVersionResult{Detected: true}}
	client, err := NewClient(root, allowLock{}, invoker, &auditCapture{})
	if err != nil {
		t.Fatal(err)
	}
	response, metadata, err := client.ExecuteWithMetadata(context.Background(), nginxDetectRequest())
	if err != nil || !response.OK || metadata.HelperIdentityRevision != invoker.Identity || metadata.CapabilityRevision != capabilities.Revision {
		t.Fatalf("execution metadata did not bind the exact helper negotiation: response=%#v metadata=%#v err=%v", response, metadata, err)
	}
	invoker.Identity = strings.Repeat("e", 64)
	_, changed, err := client.ExecuteWithMetadata(context.Background(), nginxDetectRequest())
	if err != nil || changed.HelperIdentityRevision == metadata.HelperIdentityRevision {
		t.Fatal("helper identity change retained the prior execution binding")
	}
}

func TestAuditContainsNoSecretsOrRawOutput(t *testing.T) {
	root := testManagedRoot(t)
	if err := os.MkdirAll(filepath.Join(root.Path(), "revisions"), 0o750); err != nil {
		t.Fatal(err)
	}
	secret := "super-secret-token"
	candidatePath := "revisions/" + secret
	if err := os.WriteFile(filepath.Join(root.Path(), filepath.FromSlash(candidatePath)), []byte("table inet solovey_protection {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := Request{
		ProtocolVersion: ProtocolVersion,
		Correlation:     Correlation{OperationID: "op-audit", InstanceID: "instance-audit", LockRevision: 3},
		Operation:       OperationNFTValidate,
		NFTValidate:     &NFTValidateRequest{CandidatePath: candidatePath, ExpectedRevision: strings.Repeat("a", 64), ExpectedSHA256: strings.Repeat("b", 64), ExpectedSemanticSHA256: strings.Repeat("c", 64), ExpectedTimedMembershipSHA256: strings.Repeat("d", 64)},
	}
	invoker := NewMockInvoker(availableCapabilities(OperationNFTValidate))
	invoker.Responses[OperationNFTValidate] = Response{OK: true}
	audit := &auditCapture{}
	client, err := NewClient(root, allowLock{}, invoker, audit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(audit.events)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(encoded, []byte("revisions")) {
		t.Fatalf("audit contains request secret or path: %s", encoded)
	}
}

func TestNewClientRejectsMissingInvoker(t *testing.T) {
	if _, err := NewClient(testManagedRoot(t), allowLock{}, nil, &auditCapture{}); err == nil || !strings.Contains(err.Error(), "helper invoker is required") {
		t.Fatalf("missing invoker was accepted: %v", err)
	}
}

func TestOperationWithoutCurrentLockRejected(t *testing.T) {
	root := testManagedRoot(t)
	invoker := NewMockInvoker(availableCapabilities(OperationNginxDetectVersion))
	client, err := NewClient(root, rejectLock{}, invoker, &auditCapture{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Execute(context.Background(), nginxDetectRequest())
	if err == nil || response.Code != CodeMissingCapability || response.Reason != "operation_lock_required" {
		t.Fatalf("unlocked operation mapping mismatch: response=%+v err=%v", response, err)
	}
	if len(invoker.Requests) != 0 {
		t.Fatalf("unlocked operation reached helper invoker: %#v", invoker.Requests)
	}
}

func TestOperationDoesNotRunWhenAuditIsUnavailable(t *testing.T) {
	root := testManagedRoot(t)
	invoker := NewMockInvoker(availableCapabilities(OperationNginxDetectVersion))
	client, err := NewClient(root, allowLock{}, invoker, rejectAudit{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Execute(context.Background(), nginxDetectRequest())
	if err == nil || response.Code != CodeMissingCapability || response.Reason != "audit_unavailable" {
		t.Fatalf("audit failure mapping mismatch: response=%+v err=%v", response, err)
	}
	if len(invoker.Requests) != 1 || invoker.Requests[0].Operation != OperationCapabilities {
		t.Fatalf("operation ran after audit failure: %#v", invoker.Requests)
	}
}

func TestHelperVersionPolicy(t *testing.T) {
	if !compatibleHelperVersion("1.11.9") || compatibleHelperVersion("1.10.9") || compatibleHelperVersion("1.11") {
		t.Fatal("helper major/minor compatibility policy is incorrect")
	}
}

func TestManagedTableObservationIsBoundedAndReadOnly(t *testing.T) {
	root := testManagedRoot(t)
	request := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "observe-managed-table", InstanceID: "instance"}, Operation: OperationNFTObserve, NFTObserve: &NFTObserveRequest{}}
	if err := request.Validate(root); err != nil {
		t.Fatalf("read-only managed-table observation required a mutation lock: %v", err)
	}
	if !request.UnlockedReadOnly() {
		t.Fatal("managed-table observation is not marked read-only")
	}
	if kind, err := request.RequiredLockKind(); err != nil || kind != "" {
		t.Fatalf("managed-table observation acquired a mutation authority: kind=%q err=%v", kind, err)
	}
}

func TestContractEngineDoesNotInventUnavailableMutationBackends(t *testing.T) {
	root := testManagedRoot(t)
	request := nginxDetectRequest()
	response := NewContractEngine(root).Handle(request)
	if response.OK || response.Code != CodeMissingCapability || response.Reason == "" {
		t.Fatalf("contract-only engine unexpectedly enabled an operation: %+v", response)
	}
	for _, capability := range DefaultCapabilities().Capabilities {
		if capability.Operation != OperationCapabilities && capability.Available {
			t.Fatalf("mutation/backend capability unexpectedly available: %+v", capability)
		}
	}
}

func TestManagedNFTTableIsStrict(t *testing.T) {
	root := testManagedRoot(t)
	request := Request{
		ProtocolVersion: ProtocolVersion,
		Correlation:     Correlation{OperationID: "op-1", InstanceID: "instance-1", LockRevision: 1},
		Operation:       OperationNFTApply,
		NFTApply:        &NFTApplyRequest{CandidatePath: "missing.nft", RollbackArtifactPath: "rollback.json", ExpectedTable: "inet user_table"},
	}
	if err := request.Validate(root); err == nil || !strings.Contains(err.Error(), "solovey_protection") {
		t.Fatalf("arbitrary nft table was not rejected: %v", err)
	}
}

func testManagedRoot(t *testing.T) ManagedRoot {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".runtime", "server-protection")
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
	root, err := NewManagedRoot(testRuntimeAuthorityForRoot(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func testRuntimeAuthorityForRoot(t testing.TB, rootPath string) protectionruntime.RuntimeRootAuthority {
	t.Helper()
	base := filepath.Dir(filepath.Dir(filepath.Clean(rootPath)))
	authority, err := protectionruntime.RootAuthorityForDatabaseFolder(filepath.Join(base, "db"))
	if err != nil {
		t.Fatal(err)
	}
	if authority.Path() != filepath.Clean(rootPath) {
		t.Fatalf("test runtime authority = %q, root = %q", authority.Path(), rootPath)
	}
	return authority
}

func testOpenWrtRuntimeAuthority(t testing.TB) protectionruntime.RuntimeRootAuthority {
	t.Helper()
	const instance = "00112233-4455-4677-8899-aabbccddeeff"
	source := "src-" + strings.Repeat("1", 64)
	artifact := "art-" + strings.Repeat("2", 64)
	deployment := "dep-" + strings.Repeat("3", 64)
	binding, err := protectionruntime.BindOpenWrt(protectionruntime.OpenWrtInstalled(), instance, source, artifact, deployment)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := deploymentidentity.NewProcdV1(instance, source, artifact, deployment, binding.ContractRevision, binding.BindingRevision,
		"solovey-ui-panel", "solovey-ui", "panel", "/usr/lib/solovey-ui/solovey-ui", strings.Repeat("4", 64), 997, 997)
	if err != nil {
		t.Fatal(err)
	}
	runtimeRoot, err := protectionruntime.InstalledOpenWrtRuntimeRoot(owner, testOpenWrtMountProof(t))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := deploymentidentity.ExpectedProcdApplicationOwner(owner)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := deploymentidentity.NewInstalledApplicationOwnerProjection(deploymentidentity.InstalledApplicationBackendProcd, expected)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := protectionruntime.InstalledRuntimeRootAuthority(runtimeRoot, projection)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func testOpenWrtMountProof(t testing.TB) protectionruntime.RuntimeMountProofV1 {
	t.Helper()
	root := protectionruntime.OpenWrtRuntimeRoot
	fact, err := mountevidence.Parse([]byte(fmt.Sprintf("41 36 0:41 / %s rw - tmpfs tmpfs rw\n", root)), root)
	if err == nil {
		fact, err = mountevidence.BindStatfs(fact, root, false, 0x01021994)
	}
	if err != nil {
		t.Fatal(err)
	}
	proof, err := protectionruntime.NewRuntimeMountProof(root, protectionruntime.RuntimeMountVolatile, fact)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

type managedRootDirectoryInfo struct{}

func (managedRootDirectoryInfo) Name() string       { return "server-protection" }
func (managedRootDirectoryInfo) Size() int64        { return 0 }
func (managedRootDirectoryInfo) Mode() os.FileMode  { return os.ModeDir | 0o700 }
func (managedRootDirectoryInfo) ModTime() time.Time { return time.Time{} }
func (managedRootDirectoryInfo) IsDir() bool        { return true }
func (managedRootDirectoryInfo) Sys() any           { return nil }

func nginxDetectRequest() Request {
	return Request{
		ProtocolVersion:    ProtocolVersion,
		Correlation:        Correlation{OperationID: "op-nginx", InstanceID: "instance-nginx", LockRevision: 2},
		Operation:          OperationNginxDetectVersion,
		NginxDetectVersion: &NginxDetectVersionRequest{},
	}
}

func availableCapabilities(operations ...Operation) *CapabilitiesResult {
	result := DefaultCapabilities()
	for index := range result.Capabilities {
		for _, operation := range operations {
			if result.Capabilities[index].Operation == operation {
				result.Capabilities[index].Available = true
				result.Capabilities[index].Reason = ""
			}
		}
	}
	return result
}

type allowLock struct{}

func (allowLock) ValidateHelperLock(context.Context, string, string, string, int) error { return nil }

type rejectLock struct{}

func (rejectLock) ValidateHelperLock(context.Context, string, string, string, int) error {
	return errors.New("no active lock")
}

type splitLockValidator struct {
	strictErr   error
	readErr     error
	strictCalls int
	readCalls   int
}

func (v *splitLockValidator) ValidateHelperLock(context.Context, string, string, string, int) error {
	v.strictCalls++
	return v.strictErr
}

func (v *splitLockValidator) ValidateHelperReadLock(context.Context, string, string, string, int) error {
	v.readCalls++
	return v.readErr
}

type auditCapture struct{ events []AuditEvent }

func (a *auditCapture) RecordHelperAudit(_ context.Context, event AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

type rejectAudit struct{}

func (rejectAudit) RecordHelperAudit(context.Context, AuditEvent) error {
	return errors.New("audit unavailable")
}
