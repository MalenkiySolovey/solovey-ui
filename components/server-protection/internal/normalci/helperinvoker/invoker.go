// Package helperinvoker contains explicit normal-CI test support. Product
// runtime packages must never import it.
package helperinvoker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"
	"sync"

	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

type Invoker struct {
	mu                  sync.Mutex
	Capabilities        *protectionhelper.CapabilitiesResult
	Responses           map[protectionhelper.Operation]protectionhelper.Response
	Requests            []protectionhelper.Request
	Block               bool
	Support             protectionhelper.NginxSupport
	Version             protectionhelper.NginxVersionResult
	ActiveRevision      string
	ActiveSHA256        string
	Revisions           map[string]string
	RevisionListeners   map[string][]protectionhelper.NginxListener
	Calls               []protectionhelper.Operation
	Fail                map[protectionhelper.Operation]error
	FailSequence        map[protectionhelper.Operation][]error
	FailAfter           map[protectionhelper.Operation]error
	Reloads             int
	Reloaded            map[string]protectionhelper.NginxResult
	ManagedTablePresent bool
	ManagedPlanRevision string
	ManagedCandidateSHA string
	managedRollbacks    map[string]managedFirewallState
}

type managedFirewallState struct {
	present     bool
	revision    string
	sha256      string
	rollbackSHA string
}

func New(capabilities *protectionhelper.CapabilitiesResult) *Invoker {
	if capabilities == nil {
		capabilities = protectionhelper.DefaultCapabilities()
	}
	setCapabilityRevision(capabilities)
	return &Invoker{
		Capabilities:     capabilities,
		Responses:        make(map[protectionhelper.Operation]protectionhelper.Response),
		managedRollbacks: make(map[string]managedFirewallState),
		Fail:             make(map[protectionhelper.Operation]error),
		FailSequence:     make(map[protectionhelper.Operation][]error),
		FailAfter:        make(map[protectionhelper.Operation]error),
	}
}

func NewNginx() *Invoker {
	capabilities := protectionhelper.DefaultCapabilities()
	identity := protectionhelper.BinaryIdentity{Path: "/usr/sbin/nginx", TargetPath: "/usr/sbin/nginx", Device: 1, Inode: 2}
	managedRoot := path.Join(protectionruntime.Installed().RuntimeRoot, "nginx")
	support := protectionhelper.NginxSupport{
		PlatformKnown:    true,
		Linux:            true,
		Available:        true,
		Binary:           identity,
		ManagedRoot:      managedRoot,
		ControlledConfig: path.Join(managedRoot, "loader.conf"),
		MasterPID:        100,
	}
	capabilities.Nginx = support
	for index := range capabilities.Capabilities {
		switch capabilities.Capabilities[index].Operation {
		case protectionhelper.OperationNginxDetectVersion,
			protectionhelper.OperationNginxValidate,
			protectionhelper.OperationNginxInstall,
			protectionhelper.OperationNginxSwitch,
			protectionhelper.OperationNginxReload,
			protectionhelper.OperationNginxVerify,
			protectionhelper.OperationNginxRestore:
			capabilities.Capabilities[index].Available = true
			capabilities.Capabilities[index].Reason = ""
		}
	}
	setCapabilityRevision(capabilities)
	return &Invoker{
		Capabilities:      capabilities,
		Responses:         make(map[protectionhelper.Operation]protectionhelper.Response),
		Support:           support,
		Version:           protectionhelper.NginxVersionResult{Detected: true, Version: "1.26.0", Modules: []string{"ssl_preread", "stream"}, Binary: identity},
		Revisions:         make(map[string]string),
		RevisionListeners: make(map[string][]protectionhelper.NginxListener),
		Fail:              make(map[protectionhelper.Operation]error),
		FailSequence:      make(map[protectionhelper.Operation][]error),
		FailAfter:         make(map[protectionhelper.Operation]error),
		Reloaded:          make(map[string]protectionhelper.NginxResult),
	}
}

func (i *Invoker) Invoke(ctx context.Context, request protectionhelper.Request) (protectionhelper.Response, protectionhelper.InvocationFacts, error) {
	i.mu.Lock()
	i.Requests = append(i.Requests, request)
	block := i.Block
	if i.Support.Available {
		support := i.Support
		support.ActiveRevision = i.ActiveRevision
		support.ActiveSHA256 = i.ActiveSHA256
		support.Listeners = append([]protectionhelper.NginxListener(nil), i.RevisionListeners[i.ActiveRevision]...)
		i.Capabilities.Nginx = support
	}
	setCapabilityRevision(i.Capabilities)
	capabilities := i.Capabilities
	response, explicit := i.Responses[request.Operation]
	i.mu.Unlock()

	if block {
		<-ctx.Done()
		return protectionhelper.Response{}, protectionhelper.InvocationFacts{ExitClass: "canceled"}, ctx.Err()
	}
	if request.Operation == protectionhelper.OperationCapabilities {
		return protectionhelper.Response{
			ProtocolVersion: protectionhelper.ProtocolVersion,
			HelperVersion:   protectionhelper.HelperVersion,
			Correlation:     request.Correlation,
			Operation:       request.Operation,
			OK:              true,
			Capabilities:    capabilities,
		}, facts(), nil
	}
	if explicit {
		return i.explicitResponse(request, response), facts(), nil
	}
	if i.Support.Available {
		return i.nginxResponse(request), facts(), nil
	}
	return typedResponse(request, false, protectionhelper.CodeMissingCapability, "mock_response_missing"), facts(), nil
}

func (i *Invoker) explicitResponse(request protectionhelper.Request, response protectionhelper.Response) protectionhelper.Response {
	i.mu.Lock()
	defer i.mu.Unlock()
	response.ProtocolVersion = protectionhelper.ProtocolVersion
	response.HelperVersion = protectionhelper.HelperVersion
	response.Correlation = request.Correlation
	response.Operation = request.Operation
	if response.OK && response.NFT != nil && request.Operation == protectionhelper.OperationNFTApply {
		if request.NFTApply.ExpectedPreviousTablePresent != i.ManagedTablePresent || request.NFTApply.ExpectedPreviousRevision != i.ManagedPlanRevision || request.NFTApply.ExpectedPreviousSHA256 != i.ManagedCandidateSHA {
			return typedResponse(request, false, protectionhelper.CodeValidationFailed, "managed_table_fence_mismatch")
		}
		rollbackSHA := firewallRollbackSHA(i.ManagedTablePresent, i.ManagedCandidateSHA)
		i.managedRollbacks[request.NFTApply.RollbackArtifactPath] = managedFirewallState{present: i.ManagedTablePresent, revision: i.ManagedPlanRevision, sha256: i.ManagedCandidateSHA, rollbackSHA: rollbackSHA}
		i.ManagedTablePresent, i.ManagedPlanRevision, i.ManagedCandidateSHA = true, request.NFTApply.ExpectedRevision, request.NFTApply.ExpectedSHA256
		if err := i.FailAfter[request.Operation]; err != nil {
			delete(i.FailAfter, request.Operation)
			return typedFailure(request, err)
		}
	}
	if response.OK && response.NFT == nil {
		switch request.Operation {
		case protectionhelper.OperationNFTValidate:
			response.NFT = &protectionhelper.NFTResult{CandidateSHA256: request.NFTValidate.ExpectedSHA256, PreviousTablePresent: i.ManagedTablePresent, PreviousRevision: i.ManagedPlanRevision, PreviousSHA256: i.ManagedCandidateSHA}
		case protectionhelper.OperationNFTApply:
			if request.NFTApply.ExpectedPreviousTablePresent != i.ManagedTablePresent || request.NFTApply.ExpectedPreviousRevision != i.ManagedPlanRevision || request.NFTApply.ExpectedPreviousSHA256 != i.ManagedCandidateSHA {
				return typedResponse(request, false, protectionhelper.CodeValidationFailed, "managed_table_fence_mismatch")
			}
			rollbackSHA := strings.Repeat("a", 64)
			if request.NFTApply.ExpectedPreviousTablePresent {
				rollbackSHA = request.NFTApply.ExpectedPreviousSHA256
			}
			i.managedRollbacks[request.NFTApply.RollbackArtifactPath] = managedFirewallState{present: i.ManagedTablePresent, revision: i.ManagedPlanRevision, sha256: i.ManagedCandidateSHA, rollbackSHA: rollbackSHA}
			i.ManagedTablePresent, i.ManagedPlanRevision, i.ManagedCandidateSHA = true, request.NFTApply.ExpectedRevision, request.NFTApply.ExpectedSHA256
			if err := i.FailAfter[request.Operation]; err != nil {
				delete(i.FailAfter, request.Operation)
				return typedFailure(request, err)
			}
			response.NFT = &protectionhelper.NFTResult{ManagedTablePresent: true, AppliedRevision: request.NFTApply.ExpectedRevision, CandidateSHA256: request.NFTApply.ExpectedSHA256, RollbackSHA256: rollbackSHA, PreviousRevision: request.NFTApply.ExpectedPreviousRevision, PreviousSHA256: request.NFTApply.ExpectedPreviousSHA256, PreviousTablePresent: request.NFTApply.ExpectedPreviousTablePresent}
		case protectionhelper.OperationNFTRollback:
			previous, ok := i.managedRollbacks[request.NFTRollback.RollbackArtifactPath]
			deleteSHA := sha256.Sum256([]byte("delete table inet solovey_protection\n"))
			if ok && request.NFTRollback.ExpectedCurrentRevision == i.ManagedPlanRevision && (request.NFTRollback.ExpectedSHA256 == "" || request.NFTRollback.ExpectedSHA256 == previous.rollbackSHA) {
				i.ManagedTablePresent, i.ManagedPlanRevision, i.ManagedCandidateSHA = previous.present, previous.revision, previous.sha256
				delete(i.managedRollbacks, request.NFTRollback.RollbackArtifactPath)
				response.NFT = &protectionhelper.NFTResult{ManagedTablePresent: previous.present, RollbackSHA256: previous.rollbackSHA}
				break
			}
			if request.NFTRollback.ExpectedSHA256 == hex.EncodeToString(deleteSHA[:]) && request.NFTRollback.ExpectedCurrentRevision == i.ManagedPlanRevision {
				i.ManagedTablePresent, i.ManagedPlanRevision, i.ManagedCandidateSHA = false, "", ""
				response.NFT = &protectionhelper.NFTResult{ManagedTablePresent: false, RollbackSHA256: request.NFTRollback.ExpectedSHA256}
				break
			}
			return typedResponse(request, false, protectionhelper.CodeValidationFailed, "managed_table_rollback_fence_mismatch")
		}
	}
	return response
}

func firewallRollbackSHA(present bool, candidateSHA string) string {
	if present {
		return candidateSHA
	}
	digest := sha256.Sum256([]byte("delete table inet solovey_protection\n"))
	return hex.EncodeToString(digest[:])
}

func (i *Invoker) nginxResponse(request protectionhelper.Request) protectionhelper.Response {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Calls = append(i.Calls, request.Operation)
	if err := i.before(request.Operation); err != nil {
		return typedFailure(request, err)
	}

	response := typedResponse(request, true, "", "")
	switch request.Operation {
	case protectionhelper.OperationNginxDetectVersion:
		value := i.Version
		response.NginxVersion = &value
	case protectionhelper.OperationNginxValidate:
		if request.NginxValidate.ExpectedBinary != i.Support.Binary {
			return typedFailure(request, errors.New("binary mismatch"))
		}
		response.Nginx = &protectionhelper.NginxResult{Revision: request.NginxValidate.ExpectedRevision, SHA256: request.NginxValidate.ExpectedSHA256, Binary: i.Support.Binary, Diagnostics: []string{"candidate_validation_passed"}}
	case protectionhelper.OperationNginxInstall:
		item := request.NginxInstall
		if current, ok := i.Revisions[item.ExpectedRevision]; ok && current != item.ExpectedSHA256 {
			return typedFailure(request, errors.New("revision mismatch"))
		}
		i.Revisions[item.ExpectedRevision] = item.ExpectedSHA256
		i.RevisionListeners[item.ExpectedRevision] = append([]protectionhelper.NginxListener(nil), item.Listeners...)
		response.Nginx = &protectionhelper.NginxResult{Revision: item.ExpectedRevision, SHA256: item.ExpectedSHA256}
	case protectionhelper.OperationNginxSwitch:
		item := request.NginxSwitch
		if i.ActiveRevision != item.ExpectedPreviousRevision || i.Revisions[item.TargetRevision] != item.ExpectedSHA256 {
			return typedFailure(request, errors.New("active mismatch"))
		}
		previous, previousSHA := i.ActiveRevision, i.ActiveSHA256
		i.ActiveRevision, i.ActiveSHA256 = item.TargetRevision, item.ExpectedSHA256
		if err := i.FailAfter[request.Operation]; err != nil {
			return typedFailure(request, err)
		}
		response.Nginx = &protectionhelper.NginxResult{Revision: item.TargetRevision, SHA256: item.ExpectedSHA256, PreviousRevision: previous, PreviousSHA256: previousSHA}
	case protectionhelper.OperationNginxReload:
		item := request.NginxReload
		if item.ExpectedBinary != i.Support.Binary || i.ActiveRevision != item.ExpectedRevision || i.ActiveSHA256 != item.ExpectedSHA256 {
			return typedFailure(request, errors.New("active or binary mismatch"))
		}
		key := request.Correlation.OperationID + ":" + item.ExpectedRevision
		if existing, ok := i.Reloaded[key]; ok {
			copy := existing
			copy.Diagnostics = []string{"reload_idempotent_replay"}
			response.Nginx = &copy
			break
		}
		i.Reloads++
		result := protectionhelper.NginxResult{Revision: item.ExpectedRevision, SHA256: item.ExpectedSHA256, Binary: i.Support.Binary, MasterPID: 100, WorkerPIDs: []int{200 + i.Reloads}}
		i.Reloaded[key] = result
		response.Nginx = &result
	case protectionhelper.OperationNginxVerify:
		item := request.NginxVerify
		if i.ActiveRevision != item.ExpectedRevision || i.ActiveSHA256 != item.ExpectedSHA256 || item.ExpectedBinary != i.Support.Binary || !equalListeners(item.Listeners, i.RevisionListeners[i.ActiveRevision]) {
			return typedFailure(request, errors.New("active, binary, or listener mismatch"))
		}
		response.Nginx = &protectionhelper.NginxResult{Revision: item.ExpectedRevision, SHA256: item.ExpectedSHA256, Binary: i.Support.Binary, MasterPID: 100, WorkerPIDs: []int{201}, ListenersMatched: true, Diagnostics: []string{"active_revision_verified", "process_identity_verified", "listeners_verified"}}
	case protectionhelper.OperationNginxRestore:
		item := request.NginxRestore
		if i.ActiveRevision != item.ExpectedCurrentRevision || i.Revisions[item.PreviousRevision] != item.ExpectedSHA256 {
			return typedFailure(request, errors.New("wrong previous revision"))
		}
		current, currentSHA := i.ActiveRevision, i.ActiveSHA256
		i.ActiveRevision, i.ActiveSHA256 = item.PreviousRevision, item.ExpectedSHA256
		response.Nginx = &protectionhelper.NginxResult{Revision: item.PreviousRevision, SHA256: item.ExpectedSHA256, PreviousRevision: current, PreviousSHA256: currentSHA}
	default:
		return typedResponse(request, false, protectionhelper.CodeMissingCapability, "missing_capability")
	}
	return response
}

func (i *Invoker) before(operation protectionhelper.Operation) error {
	if sequence := i.FailSequence[operation]; len(sequence) > 0 {
		failure := sequence[0]
		i.FailSequence[operation] = sequence[1:]
		return failure
	}
	return i.Fail[operation]
}

func typedFailure(request protectionhelper.Request, err error) protectionhelper.Response {
	code := protectionhelper.CodeValidationFailed
	reason := "managed_operation_failed"
	if errors.Is(err, context.DeadlineExceeded) {
		code, reason = protectionhelper.CodeTimeout, "timeout"
	} else if errors.Is(err, context.Canceled) {
		code, reason = protectionhelper.CodeCanceled, "canceled"
	}
	return typedResponse(request, false, code, reason)
}

func typedResponse(request protectionhelper.Request, ok bool, code protectionhelper.ErrorCode, reason string) protectionhelper.Response {
	return protectionhelper.Response{
		ProtocolVersion: protectionhelper.ProtocolVersion,
		HelperVersion:   protectionhelper.HelperVersion,
		Correlation:     request.Correlation,
		Operation:       request.Operation,
		OK:              ok,
		Code:            code,
		Reason:          reason,
	}
}

func facts() protectionhelper.InvocationFacts {
	return protectionhelper.InvocationFacts{ExitClass: "normal_ci"}
}

func setCapabilityRevision(result *protectionhelper.CapabilitiesResult) {
	copy := *result
	copy.Revision = ""
	data, _ := json.Marshal(copy)
	digest := sha256.Sum256(data)
	result.Revision = hex.EncodeToString(digest[:])
}

func equalListeners(left, right []protectionhelper.NginxListener) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]protectionhelper.NginxListener(nil), left...)
	rightCopy := append([]protectionhelper.NginxListener(nil), right...)
	sort.Slice(leftCopy, func(a, b int) bool {
		if leftCopy[a].Port != leftCopy[b].Port {
			return leftCopy[a].Port < leftCopy[b].Port
		}
		return leftCopy[a].Address < leftCopy[b].Address
	})
	sort.Slice(rightCopy, func(a, b int) bool {
		if rightCopy[a].Port != rightCopy[b].Port {
			return rightCopy[a].Port < rightCopy[b].Port
		}
		return rightCopy[a].Address < rightCopy[b].Address
	})
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}
