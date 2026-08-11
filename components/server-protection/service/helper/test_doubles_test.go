package helper

import (
	"context"
	"errors"
	"path"
	"strings"
	"sync"

	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
)

type MockInvoker struct {
	mu           sync.Mutex
	Identity     string
	Capabilities *CapabilitiesResult
	Responses    map[Operation]Response
	Requests     []Request
	Block        bool
}

func NewMockInvoker(capabilities *CapabilitiesResult) *MockInvoker {
	if capabilities == nil {
		capabilities = DefaultCapabilities()
	}
	setCapabilityRevision(capabilities)
	return &MockInvoker{Identity: strings.Repeat("d", 64), Capabilities: capabilities, Responses: make(map[Operation]Response)}
}

func (m *MockInvoker) HelperIdentityRevision() string { return m.Identity }

func (m *MockInvoker) Invoke(ctx context.Context, request Request) (Response, InvocationFacts, error) {
	m.mu.Lock()
	m.Requests = append(m.Requests, request)
	block := m.Block
	capabilities := m.Capabilities
	setCapabilityRevision(capabilities)
	response, exists := m.Responses[request.Operation]
	m.mu.Unlock()
	if block {
		<-ctx.Done()
		return Response{}, InvocationFacts{ExitClass: "canceled"}, ctx.Err()
	}
	if request.Operation == OperationCapabilities {
		return Response{
			ProtocolVersion: ProtocolVersion, HelperVersion: HelperVersion,
			Correlation: request.Correlation, Operation: request.Operation, OK: true,
			Capabilities: capabilities,
		}, InvocationFacts{ExitClass: "mock"}, nil
	}
	if exists {
		response.ProtocolVersion = ProtocolVersion
		response.HelperVersion = HelperVersion
		response.Correlation = request.Correlation
		response.Operation = request.Operation
		if response.OK && response.NFT == nil {
			switch request.Operation {
			case OperationNFTValidate:
				response.NFT = &NFTResult{CandidateSHA256: request.NFTValidate.ExpectedSHA256}
			case OperationNFTApply:
				rollbackSHA := strings.Repeat("a", 64)
				if request.NFTApply.ExpectedPreviousTablePresent {
					rollbackSHA = request.NFTApply.ExpectedPreviousSHA256
				}
				response.NFT = &NFTResult{ManagedTablePresent: true, AppliedRevision: request.NFTApply.ExpectedRevision, CandidateSHA256: request.NFTApply.ExpectedSHA256, RollbackSHA256: rollbackSHA, PreviousRevision: request.NFTApply.ExpectedPreviousRevision, PreviousSHA256: request.NFTApply.ExpectedPreviousSHA256, PreviousTablePresent: request.NFTApply.ExpectedPreviousTablePresent}
			case OperationNFTRollback:
				response.NFT = &NFTResult{RollbackSHA256: strings.Repeat("a", 64)}
			}
		}
		return response, InvocationFacts{ExitClass: "mock"}, nil
	}
	return responseError(request, CodeMissingCapability, "mock_response_missing"), InvocationFacts{ExitClass: "mock"}, nil
}

type FakeListenerExecutor struct {
	Requests []ListenerProbeRequest
	Results  []ListenerProbeResult
	Err      error
}

func (f *FakeListenerExecutor) Probe(_ context.Context, request ListenerProbeRequest) (*ListenerProbeResult, error) {
	f.Requests = append(f.Requests, request)
	if f.Err != nil {
		return nil, f.Err
	}
	if len(f.Results) == 0 {
		return &ListenerProbeResult{Reachable: true, OwnerMatched: true, OwnerClass: request.ExpectedOwner, Detail: "fake_listener_reachable"}, nil
	}
	result := f.Results[0]
	f.Results = f.Results[1:]
	return &result, nil
}

type FakeNginxExecutor struct {
	mu                sync.Mutex
	Support           NginxSupport
	Version           NginxVersionResult
	ActiveRevision    string
	ActiveSHA256      string
	Revisions         map[string]string
	RevisionListeners map[string][]NginxListener
	Calls             []Operation
	Fail              map[Operation]error
	FailSequence      map[Operation][]error
	FailAfter         map[Operation]error
	Reloads           int
	Reloaded          map[string]NginxResult
}

func NewFakeNginxExecutor() *FakeNginxExecutor {
	managedRoot := path.Join(protectionruntime.Installed().RuntimeRoot, "nginx")
	identity := BinaryIdentity{Path: "/usr/sbin/nginx", TargetPath: "/usr/sbin/nginx", Device: 1, Inode: 2}
	return &FakeNginxExecutor{Support: NginxSupport{PlatformKnown: true, Linux: true, Available: true, Binary: identity, ManagedRoot: managedRoot, ControlledConfig: path.Join(managedRoot, "loader.conf"), MasterPID: 100}, Version: NginxVersionResult{Detected: true, Version: "1.26.0", Modules: []string{"ssl_preread", "stream"}, Binary: identity}, Revisions: map[string]string{}, RevisionListeners: map[string][]NginxListener{}, Fail: map[Operation]error{}, FailSequence: map[Operation][]error{}, FailAfter: map[Operation]error{}, Reloaded: map[string]NginxResult{}}
}

func (f *FakeNginxExecutor) before(operation Operation) error {
	f.Calls = append(f.Calls, operation)
	if sequence := f.FailSequence[operation]; len(sequence) > 0 {
		failure := sequence[0]
		f.FailSequence[operation] = sequence[1:]
		return failure
	}
	return f.Fail[operation]
}

func (f *FakeNginxExecutor) Detect(context.Context) NginxSupport {
	f.mu.Lock()
	defer f.mu.Unlock()
	value := f.Support
	value.ActiveRevision, value.ActiveSHA256 = f.ActiveRevision, f.ActiveSHA256
	value.Listeners = append([]NginxListener(nil), f.RevisionListeners[f.ActiveRevision]...)
	return value
}

func (f *FakeNginxExecutor) DetectVersion(context.Context) (*NginxVersionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.before(OperationNginxDetectVersion); err != nil {
		return nil, err
	}
	value := f.Version
	return &value, nil
}

func (f *FakeNginxExecutor) Validate(_ context.Context, _ Correlation, r NginxValidateRequest) (*NginxResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.before(OperationNginxValidate); err != nil {
		return nil, err
	}
	if r.ExpectedBinary != f.Support.Binary {
		return nil, errors.New("binary mismatch")
	}
	return &NginxResult{Revision: r.ExpectedRevision, SHA256: r.ExpectedSHA256, Binary: f.Support.Binary, Diagnostics: []string{"candidate_validation_passed"}}, nil
}

func (f *FakeNginxExecutor) Install(_ context.Context, _ Correlation, r NginxInstallRequest) (*NginxResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.before(OperationNginxInstall); err != nil {
		return nil, err
	}
	if current, ok := f.Revisions[r.ExpectedRevision]; ok && current != r.ExpectedSHA256 {
		return nil, errors.New("revision mismatch")
	}
	f.Revisions[r.ExpectedRevision] = r.ExpectedSHA256
	f.RevisionListeners[r.ExpectedRevision] = append([]NginxListener(nil), r.Listeners...)
	return &NginxResult{Revision: r.ExpectedRevision, SHA256: r.ExpectedSHA256}, nil
}

func (f *FakeNginxExecutor) Switch(_ context.Context, _ Correlation, r NginxSwitchRequest) (*NginxResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.before(OperationNginxSwitch); err != nil {
		return nil, err
	}
	if f.ActiveRevision != r.ExpectedPreviousRevision || f.Revisions[r.TargetRevision] != r.ExpectedSHA256 {
		return nil, errors.New("active mismatch")
	}
	previous, sha := f.ActiveRevision, f.ActiveSHA256
	f.ActiveRevision, f.ActiveSHA256 = r.TargetRevision, r.ExpectedSHA256
	if err := f.FailAfter[OperationNginxSwitch]; err != nil {
		return nil, err
	}
	return &NginxResult{Revision: r.TargetRevision, SHA256: r.ExpectedSHA256, PreviousRevision: previous, PreviousSHA256: sha}, nil
}

func (f *FakeNginxExecutor) Reload(_ context.Context, correlation Correlation, r NginxReloadRequest) (*NginxResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.before(OperationNginxReload); err != nil {
		return nil, err
	}
	if r.ExpectedBinary != f.Support.Binary {
		return nil, errors.New("binary mismatch")
	}
	key := correlation.OperationID + ":" + r.ExpectedRevision
	if existing, ok := f.Reloaded[key]; ok {
		copy := existing
		copy.Diagnostics = []string{"reload_idempotent_replay"}
		return &copy, nil
	}
	if f.ActiveRevision != r.ExpectedRevision || f.ActiveSHA256 != r.ExpectedSHA256 {
		return nil, errors.New("active mismatch")
	}
	f.Reloads++
	result := NginxResult{Revision: r.ExpectedRevision, SHA256: r.ExpectedSHA256, Binary: f.Support.Binary, MasterPID: 100, WorkerPIDs: []int{200 + f.Reloads}}
	f.Reloaded[key] = result
	return &result, nil
}

func (f *FakeNginxExecutor) Verify(_ context.Context, _ Correlation, r NginxVerifyRequest) (*NginxResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.before(OperationNginxVerify); err != nil {
		return nil, err
	}
	if f.ActiveRevision != r.ExpectedRevision || f.ActiveSHA256 != r.ExpectedSHA256 {
		return nil, errors.New("active mismatch")
	}
	if r.ExpectedBinary != f.Support.Binary {
		return nil, errors.New("binary mismatch")
	}
	if !equalNginxListeners(r.Listeners, f.RevisionListeners[f.ActiveRevision]) {
		return nil, errors.New("listener mismatch")
	}
	return &NginxResult{Revision: r.ExpectedRevision, SHA256: r.ExpectedSHA256, Binary: f.Support.Binary, MasterPID: 100, WorkerPIDs: []int{201}, ListenersMatched: true, Diagnostics: []string{"active_revision_verified", "process_identity_verified", "listeners_verified"}}, nil
}

func (f *FakeNginxExecutor) Restore(_ context.Context, _ Correlation, r NginxRestoreRequest) (*NginxResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.before(OperationNginxRestore); err != nil {
		return nil, err
	}
	if f.ActiveRevision != r.ExpectedCurrentRevision || f.Revisions[r.PreviousRevision] != r.ExpectedSHA256 {
		return nil, errors.New("wrong previous revision")
	}
	current, sha := f.ActiveRevision, f.ActiveSHA256
	f.ActiveRevision, f.ActiveSHA256 = r.PreviousRevision, r.ExpectedSHA256
	return &NginxResult{Revision: r.PreviousRevision, SHA256: r.ExpectedSHA256, PreviousRevision: current, PreviousSHA256: sha}, nil
}

func newContractEngineWithExecutors(root ManagedRoot, executor NFTExecutor, listener ListenerExecutor) ContractEngine {
	return ContractEngine{root: root, executor: executor, listenerExecutor: listener}
}

func newContractEngineWithExecutor(root ManagedRoot, executor NFTExecutor) ContractEngine {
	return ContractEngine{root: root, executor: executor}
}

func newContractEngineWithBackends(root ManagedRoot, nft NFTExecutor, nginx NginxExecutor, listener ListenerExecutor) ContractEngine {
	return ContractEngine{root: root, executor: nft, nginxExecutor: nginx, listenerExecutor: listener}
}
