package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTypedNginxBackendSequenceAndExactRollback(t *testing.T) {
	root, candidatePath, revision, candidateSHA := nginxTestRoot(t)
	nginx := NewFakeNginxExecutor()
	previous, previousSHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	nginx.ActiveRevision, nginx.ActiveSHA256 = previous, previousSHA
	nginx.Revisions[previous] = previousSHA
	nginx.RevisionListeners[previous] = []NginxListener{{Address: "0.0.0.0", Port: 443}}
	engine := newContractEngineWithBackends(root, nil, nginx)
	correlation := Correlation{OperationID: "operation-nginx", InstanceID: "instance-nginx", LockRevision: 1}
	identity := nginx.Support.Binary
	requests := []Request{
		{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxValidate, NginxValidate: &NginxValidateRequest{CandidatePath: candidatePath, ExpectedRevision: revision, ExpectedSHA256: candidateSHA, ExpectedBinary: identity}},
		{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxInstall, NginxInstall: &NginxInstallRequest{CandidatePath: candidatePath, ExpectedRevision: revision, ExpectedSHA256: candidateSHA, Listeners: []NginxListener{{Address: "0.0.0.0", Port: 8443}}}},
		{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxSwitch, NginxSwitch: &NginxSwitchRequest{ExpectedPreviousRevision: previous, TargetRevision: revision, ExpectedSHA256: candidateSHA}},
		{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxReload, NginxReload: &NginxReloadRequest{ExpectedRevision: revision, ExpectedSHA256: candidateSHA, ExpectedBinary: identity}},
		{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxVerify, NginxVerify: &NginxVerifyRequest{ExpectedRevision: revision, ExpectedSHA256: candidateSHA, ExpectedBinary: identity, Listeners: []NginxListener{{Address: "0.0.0.0", Port: 8443}}}},
		{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxRestore, NginxRestore: &NginxRestoreRequest{ExpectedCurrentRevision: revision, PreviousRevision: previous, ExpectedSHA256: previousSHA}},
	}
	for _, request := range requests {
		response := engine.Handle(request)
		if !response.OK || response.Nginx == nil {
			t.Fatalf("%s response=%#v", request.Operation, response)
		}
	}
	if nginx.ActiveRevision != previous || nginx.ActiveSHA256 != previousSHA || nginx.Reloads != 1 {
		t.Fatalf("active=%s sha=%s reloads=%d", nginx.ActiveRevision, nginx.ActiveSHA256, nginx.Reloads)
	}
	wrong := engine.Handle(Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxRestore, NginxRestore: &NginxRestoreRequest{ExpectedCurrentRevision: previous, PreviousRevision: strings.Repeat("d", 64), ExpectedSHA256: previousSHA}})
	if wrong.OK || wrong.Code != CodeValidationFailed {
		t.Fatalf("wrong previous revision accepted: %#v", wrong)
	}
}

func TestNginxContractRejectsTraversalSymlinksAndArbitraryPayload(t *testing.T) {
	root, candidatePath, revision, candidateSHA := nginxTestRoot(t)
	identity := NewFakeNginxExecutor().Support.Binary
	for name, path := range map[string]string{"traversal": "../candidate.conf", "absolute": filepath.Join(root.Path(), "revisions", "candidate.conf"), "arbitrary": "../../etc/nginx/nginx.conf"} {
		t.Run(name, func(t *testing.T) {
			request := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-path", InstanceID: "instance", LockRevision: 1}, Operation: OperationNginxValidate, NginxValidate: &NginxValidateRequest{CandidatePath: path, ExpectedRevision: revision, ExpectedSHA256: candidateSHA, ExpectedBinary: identity}}
			if err := request.Validate(root); !errors.Is(err, ErrManagedPathForbidden) {
				t.Fatalf("path %q err=%v", path, err)
			}
		})
	}
	link := filepath.Join(root.Path(), "candidate-link.conf")
	target, _ := root.Resolve(candidatePath, true)
	if err := os.Symlink(target, link); err == nil {
		request := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-link", InstanceID: "instance", LockRevision: 1}, Operation: OperationNginxValidate, NginxValidate: &NginxValidateRequest{CandidatePath: "candidate-link.conf", ExpectedRevision: revision, ExpectedSHA256: candidateSHA, ExpectedBinary: identity}}
		if err := request.Validate(root); !errors.Is(err, ErrManagedPathForbidden) {
			t.Fatalf("symlink candidate accepted: %v", err)
		}
	}
	malformed := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-shape", InstanceID: "instance", LockRevision: 1}, Operation: OperationNginxReload, NginxReload: &NginxReloadRequest{ExpectedRevision: revision, ExpectedSHA256: candidateSHA, ExpectedBinary: identity}, Artifact: &ArtifactRequest{Scope: ArtifactScopeNginx, Action: ArtifactRemove, Path: candidatePath}}
	if err := malformed.Validate(root); err == nil || !strings.Contains(err.Error(), "exactly one typed") {
		t.Fatalf("arbitrary second payload accepted: %v", err)
	}
	t.Run("symlink parent", func(t *testing.T) {
		realDir := filepath.Join(root.Path(), "real-parent")
		if err := os.MkdirAll(realDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(realDir, "candidate.conf"), []byte("# generated\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realDir, filepath.Join(root.Path(), "linked-parent")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		request := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-parent", InstanceID: "instance", LockRevision: 1}, Operation: OperationNginxValidate, NginxValidate: &NginxValidateRequest{CandidatePath: "linked-parent/candidate.conf", ExpectedRevision: revision, ExpectedSHA256: candidateSHA, ExpectedBinary: identity}}
		if err := request.Validate(root); !errors.Is(err, ErrManagedPathForbidden) {
			t.Fatalf("symlink parent accepted: %v", err)
		}
	})
	t.Run("symlink revision", func(t *testing.T) {
		revisions := filepath.Join(root.Path(), "nginx", "revisions")
		target := filepath.Join(revisions, strings.Repeat("e", 64))
		if err := os.MkdirAll(target, 0o700); err != nil {
			t.Fatal(err)
		}
		linkRevision := strings.Repeat("f", 64)
		if err := os.Symlink(target, filepath.Join(revisions, linkRevision)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := (&systemNginxExecutor{root: root}).revisionSHA(linkRevision); err == nil || !strings.Contains(err.Error(), "symlinked") {
			t.Fatalf("symlink revision accepted: %v", err)
		}
	})
}

func TestNginxFakeContractVerifiesCandidateBytesBeforeBackend(t *testing.T) {
	root, candidatePath, revision, _ := nginxTestRoot(t)
	nginx := NewFakeNginxExecutor()
	request := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-sha", InstanceID: "instance", LockRevision: 1}, Operation: OperationNginxValidate, NginxValidate: &NginxValidateRequest{CandidatePath: candidatePath, ExpectedRevision: revision, ExpectedSHA256: strings.Repeat("d", 64), ExpectedBinary: nginx.Support.Binary}}
	response := newContractEngineWithBackends(root, nil, nginx).Handle(request)
	if response.OK || response.Code != CodeValidationFailed || containsOperation(nginx.Calls, OperationNginxValidate) {
		t.Fatalf("candidate SHA mismatch reached backend: response=%#v calls=%v", response, nginx.Calls)
	}
}

func TestFakeNginxInstallNeverTouchesUserConfiguration(t *testing.T) {
	root, candidatePath, revision, candidateSHA := nginxTestRoot(t)
	userConfig := filepath.Join(t.TempDir(), "nginx.conf")
	want := []byte("# user-owned configuration remains outside the managed root\n")
	if err := os.WriteFile(userConfig, want, 0o600); err != nil {
		t.Fatal(err)
	}
	nginx := NewFakeNginxExecutor()
	request := Request{ProtocolVersion: ProtocolVersion, Correlation: Correlation{OperationID: "operation-fake-scope", InstanceID: "instance", LockRevision: 1}, Operation: OperationNginxInstall, NginxInstall: &NginxInstallRequest{CandidatePath: candidatePath, ExpectedRevision: revision, ExpectedSHA256: candidateSHA, Listeners: []NginxListener{{Address: "0.0.0.0", Port: 8443}}}}
	response := newContractEngineWithBackends(root, nil, nginx).Handle(request)
	got, err := os.ReadFile(userConfig)
	if !response.OK || err != nil || string(got) != string(want) {
		t.Fatalf("response=%#v userConfig=%q err=%v", response, got, err)
	}
}

func TestManagedInstallLeavesUserNginxConfigurationUnchanged(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("system nginx managed-root permission contract is Linux-only")
	}
	root, candidatePath, revision, candidateSHA := nginxTestRoot(t)
	if err := ensurePrivateDirectory(filepath.Join(root.Path(), "nginx")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root.Path(), "nginx")); err != nil || !platformRootOwned(info) {
		t.Skip("system managed-root mutation requires a root-owned Linux fixture")
	}
	userConfig := filepath.Join(t.TempDir(), "nginx.conf")
	want := []byte("# user-managed nginx configuration\n")
	if err := os.WriteFile(userConfig, want, 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &systemNginxExecutor{root: root}
	result, err := executor.Install(context.Background(), Correlation{OperationID: "operation-managed-install", InstanceID: "instance", LockRevision: 1}, NginxInstallRequest{CandidatePath: candidatePath, ExpectedRevision: revision, ExpectedSHA256: candidateSHA, Listeners: []NginxListener{{Address: "0.0.0.0", Port: 8443}}})
	if err != nil || result.Revision != revision {
		t.Fatalf("install=%#v err=%v", result, err)
	}
	got, err := os.ReadFile(userConfig)
	if err != nil || string(got) != string(want) {
		t.Fatalf("user nginx config changed: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root.Path(), "nginx", "revisions", revision, "stream.conf")); err != nil {
		t.Fatalf("managed revision missing: %v", err)
	}
}

func containsOperation(values []Operation, wanted Operation) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestNginxBackendTimeoutAndOversizedDiagnosticsAreTypedFailures(t *testing.T) {
	root, candidatePath, revision, candidateSHA := nginxTestRoot(t)
	nginx := NewFakeNginxExecutor()
	identity := nginx.Support.Binary
	correlation := Correlation{OperationID: "operation-failure", InstanceID: "instance", LockRevision: 1}
	request := Request{ProtocolVersion: ProtocolVersion, Correlation: correlation, Operation: OperationNginxValidate, NginxValidate: &NginxValidateRequest{CandidatePath: candidatePath, ExpectedRevision: revision, ExpectedSHA256: candidateSHA, ExpectedBinary: identity}}
	for name, failure := range map[string]error{"timeout": context.DeadlineExceeded, "oversized": errNginxOutputLimit} {
		t.Run(name, func(t *testing.T) {
			nginx.Fail[OperationNginxValidate] = failure
			response := newContractEngineWithBackends(root, nil, nginx).Handle(request)
			if response.OK {
				t.Fatalf("failure accepted: %#v", response)
			}
			if name == "timeout" && response.Code != CodeTimeout {
				t.Fatalf("timeout code=%s", response.Code)
			}
			if name == "oversized" && response.Code != CodeValidationFailed {
				t.Fatalf("oversized code=%s", response.Code)
			}
		})
	}
}

func nginxTestRoot(t *testing.T) (ManagedRoot, string, string, string) {
	t.Helper()
	rootPath := filepath.Join(t.TempDir(), ".runtime", "server-protection")
	if err := os.MkdirAll(filepath.Join(rootPath, "revisions", "operation-nginx"), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := []byte("# generated\n")
	relative := "revisions/operation-nginx/candidate.conf"
	if err := os.WriteFile(filepath.Join(rootPath, filepath.FromSlash(relative)), candidate, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := NewManagedRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	return root, relative, strings.Repeat("c", 64), sha256Hex(candidate)
}
