package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ContractEngine is the privileged side of the typed protocol. Its executor
// vocabulary can address only the Solovey-managed nft table.
type ContractEngine struct {
	root                  ManagedRoot
	executor              NFTExecutor
	nginxExecutor         NginxExecutor
	listenerOwnerExecutor ListenerOwnerExecutor
	sshRecoveryExecutor   SSHRecoveryExecutor
}

func NewContractEngine(root ManagedRoot) ContractEngine {
	return ContractEngine{root: root, executor: newSystemNFTExecutor(), nginxExecutor: newSystemNginxExecutor(root), listenerOwnerExecutor: newSystemListenerOwnerExecutor(), sshRecoveryExecutor: newSystemSSHRecoveryExecutor()}
}

func (e ContractEngine) Handle(request Request) Response {
	return e.HandleContext(context.Background(), request)
}

func (e ContractEngine) HandleContext(ctx context.Context, request Request) Response {
	response := Response{ProtocolVersion: ProtocolVersion, HelperVersion: HelperVersion, Correlation: request.Correlation, Operation: request.Operation}
	if request.ProtocolVersion != ProtocolVersion {
		response.Code, response.Reason = CodeMissingCapability, "helper_version_mismatch"
		return response
	}
	if err := request.Validate(e.root); err != nil {
		response.Code, response.Reason = CodeInvalidRequest, "request_validation_failed"
		if errors.Is(err, ErrManagedPathForbidden) {
			response.Code, response.Reason = CodePathForbidden, "path_forbidden"
		}
		return response
	}
	capabilities := e.capabilities(ctx)
	if request.Operation == OperationCapabilities {
		response.OK, response.Capabilities = true, capabilities
		return response
	}
	if !CapabilityAvailable(capabilities, request.Operation) {
		response.Code, response.Reason = CodeMissingCapability, capabilityReason(capabilities, request.Operation)
		return response
	}
	var err error
	switch request.Operation {
	case OperationNFTValidate:
		response.NFT, err = e.validate(ctx, request.Correlation, *request.NFTValidate)
	case OperationNFTApply:
		response.NFT, err = e.apply(ctx, request.Correlation, *request.NFTApply)
	case OperationNFTRollback:
		response.NFT, err = e.rollback(ctx, request.Correlation, *request.NFTRollback)
	case OperationNginxDetectVersion:
		response.NginxVersion, err = e.nginxExecutor.DetectVersion(ctx)
	case OperationNginxValidate:
		if err = e.verifyNginxCandidate(request.NginxValidate.CandidatePath, request.NginxValidate.ExpectedSHA256); err == nil {
			response.Nginx, err = e.nginxExecutor.Validate(ctx, request.Correlation, *request.NginxValidate)
		}
	case OperationNginxInstall:
		if err = e.verifyNginxCandidate(request.NginxInstall.CandidatePath, request.NginxInstall.ExpectedSHA256); err == nil {
			response.Nginx, err = e.nginxExecutor.Install(ctx, request.Correlation, *request.NginxInstall)
		}
	case OperationNginxSwitch:
		response.Nginx, err = e.nginxExecutor.Switch(ctx, request.Correlation, *request.NginxSwitch)
	case OperationNginxReload:
		response.Nginx, err = e.nginxExecutor.Reload(ctx, request.Correlation, *request.NginxReload)
	case OperationNginxVerify:
		response.Nginx, err = e.nginxExecutor.Verify(ctx, request.Correlation, *request.NginxVerify)
	case OperationNginxRestore:
		response.Nginx, err = e.nginxExecutor.Restore(ctx, request.Correlation, *request.NginxRestore)
	case OperationListenerOwnerObserve:
		response.ListenerOwner, err = e.listenerOwnerExecutor.Observe(ctx, *request.ListenerOwnerObserve)
	case OperationSSHRecoveryObserve:
		response.SSHRecovery, err = e.sshRecoveryExecutor.Observe(ctx, *request.SSHRecoveryObserve)
	default:
		response.Code, response.Reason = CodeMissingCapability, "missing_capability"
		return response
	}
	if err != nil {
		response.Code, response.Reason = CodeValidationFailed, "managed_operation_failed"
		if errors.Is(err, context.DeadlineExceeded) {
			response.Code, response.Reason = CodeTimeout, "timeout"
		} else if errors.Is(err, context.Canceled) {
			response.Code, response.Reason = CodeCanceled, "canceled"
		}
		return response
	}
	response.OK = true
	return response
}

func (e ContractEngine) verifyNginxCandidate(relative, expectedSHA string) error {
	path, err := e.root.ResolveNoSymlink(relative, true)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := copyBounded(file, MaxArtifactBytes)
	if err != nil {
		return err
	}
	if sha256Hex(data) != expectedSHA {
		return errors.New("nginx candidate SHA-256 mismatch")
	}
	return nil
}

func (e ContractEngine) capabilities(ctx context.Context) *CapabilitiesResult {
	result := DefaultCapabilities()
	if e.executor == nil {
		result.NFT = NFTSupport{Reason: "nft_capability_unknown"}
	} else {
		result.NFT = e.executor.Detect(ctx)
		if result.NFT.PlatformKnown && result.NFT.Linux && result.NFT.Available {
			for index := range result.Capabilities {
				switch result.Capabilities[index].Operation {
				case OperationNFTValidate, OperationNFTApply, OperationNFTRollback:
					result.Capabilities[index].Available = true
					result.Capabilities[index].Reason = ""
				}
			}
		}
	}
	if e.listenerOwnerExecutor == nil {
		result.ListenerOwner = ListenerOwnerSupport{Reason: "listener_owner_capability_unknown"}
	} else {
		result.ListenerOwner = e.listenerOwnerExecutor.Detect(ctx)
		if result.ListenerOwner.PlatformKnown && result.ListenerOwner.Linux && result.ListenerOwner.Available {
			for index := range result.Capabilities {
				if result.Capabilities[index].Operation == OperationListenerOwnerObserve {
					result.Capabilities[index].Available = true
					result.Capabilities[index].Reason = ""
				}
			}
		}
	}
	if e.sshRecoveryExecutor == nil {
		result.SSHRecovery = SSHRecoverySupport{Reason: "ssh_recovery_capability_unknown"}
	} else {
		result.SSHRecovery = e.sshRecoveryExecutor.Detect(ctx)
		if result.SSHRecovery.PlatformKnown && result.SSHRecovery.Linux && result.SSHRecovery.Available {
			for index := range result.Capabilities {
				if result.Capabilities[index].Operation == OperationSSHRecoveryObserve {
					result.Capabilities[index].Available = true
					result.Capabilities[index].Reason = ""
				}
			}
		}
	}
	if e.nginxExecutor == nil {
		result.Nginx = NginxSupport{PlatformKnown: false, Reason: "nginx_capability_unknown"}
	} else {
		result.Nginx = e.nginxExecutor.Detect(ctx)
		if result.Nginx.PlatformKnown && result.Nginx.Linux && result.Nginx.Available {
			for index := range result.Capabilities {
				switch result.Capabilities[index].Operation {
				case OperationNginxDetectVersion, OperationNginxValidate, OperationNginxInstall, OperationNginxSwitch, OperationNginxReload, OperationNginxVerify, OperationNginxRestore:
					result.Capabilities[index].Available = true
					result.Capabilities[index].Reason = ""
				}
			}
		}
	}
	setCapabilityRevision(result)
	return result
}

func capabilityReason(result *CapabilitiesResult, operation Operation) string {
	for _, capability := range result.Capabilities {
		if capability.Operation == operation && capability.Reason != "" {
			return capability.Reason
		}
	}
	return "missing_capability"
}

func (e ContractEngine) validate(ctx context.Context, correlation Correlation, request NFTValidateRequest) (*NFTResult, error) {
	path, err := e.root.Resolve(request.CandidatePath, true)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := validateCandidate(data, request.ExpectedRevision, request.ExpectedSHA256); err != nil {
		return nil, err
	}
	current, present, err := e.executor.ListManagedTable(ctx)
	if err != nil {
		return nil, err
	}
	previousRevision, previousSHA := "", ""
	if present {
		previousRevision, err = managedRevision(current)
		if err != nil {
			return nil, fmt.Errorf("current managed table has no proven Solovey owner: %w", err)
		}
		previousSHA = sha256Hex(current)
	}
	transaction := append([]byte(nil), data...)
	if present {
		transaction = append([]byte("delete table inet solovey_protection\n"), transaction...)
	}
	transactionPath, err := writeManagedAtomic(e.root, "operations/"+correlation.OperationID+"/validate-transaction.nft", transaction)
	if err != nil {
		return nil, err
	}
	if err := e.executor.CheckManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	return &NFTResult{CandidateSHA256: request.ExpectedSHA256, PreviousRevision: previousRevision, PreviousSHA256: previousSHA, PreviousTablePresent: present}, nil
}

func (e ContractEngine) apply(ctx context.Context, correlation Correlation, request NFTApplyRequest) (*NFTResult, error) {
	candidatePath, err := e.root.Resolve(request.CandidatePath, true)
	if err != nil {
		return nil, err
	}
	candidate, err := os.ReadFile(candidatePath)
	if err != nil {
		return nil, err
	}
	if err := validateCandidate(candidate, request.ExpectedRevision, request.ExpectedSHA256); err != nil {
		return nil, err
	}
	before, present, err := e.executor.ListManagedTable(ctx)
	if err != nil {
		return nil, err
	}
	if present != request.ExpectedPreviousTablePresent {
		return nil, errors.New("managed table presence changed after validation")
	}
	rollback := []byte("delete table inet solovey_protection\n")
	previousRevision := ""
	if present {
		previousRevision, err = managedRevision(before)
		if err != nil {
			return nil, fmt.Errorf("current managed table is not safely restorable: %w", err)
		}
		if previousRevision != request.ExpectedPreviousRevision || sha256Hex(before) != request.ExpectedPreviousSHA256 {
			return nil, errors.New("managed table identity changed after validation")
		}
		rollback = append([]byte(nil), before...)
		if len(rollback) == 0 || rollback[len(rollback)-1] != '\n' {
			rollback = append(rollback, '\n')
		}
	}
	if _, err := writeManagedAtomic(e.root, request.RollbackArtifactPath, rollback); err != nil {
		return nil, err
	}
	rollbackSHA := sha256Hex(rollback)
	if _, err := writeManagedAtomic(e.root, request.RollbackArtifactPath+".sha256", []byte(rollbackSHA+"\n")); err != nil {
		return nil, err
	}
	transaction := append([]byte(nil), candidate...)
	if present {
		transaction = append([]byte("delete table inet solovey_protection\n"), transaction...)
	}
	if err := validateManagedScope(transaction, true); err != nil {
		return nil, err
	}
	transactionPath, err := writeManagedAtomic(e.root, "operations/"+correlation.OperationID+"/apply-transaction.nft", transaction)
	if err != nil {
		return nil, err
	}
	if err := e.executor.CheckManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	if err := e.executor.ApplyManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	after, afterPresent, err := e.executor.ListManagedTable(ctx)
	if err != nil || !afterPresent {
		return nil, errors.Join(errors.New("managed table is absent after apply"), err)
	}
	if err := verifyManagedRevision(after, request.ExpectedRevision); err != nil {
		return nil, errors.New("managed table revision verification failed")
	}
	return &NFTResult{ManagedTablePresent: true, AppliedRevision: request.ExpectedRevision, CandidateSHA256: request.ExpectedSHA256, RollbackSHA256: rollbackSHA, PreviousRevision: previousRevision, PreviousSHA256: request.ExpectedPreviousSHA256, PreviousTablePresent: present}, nil
}

func (e ContractEngine) rollback(ctx context.Context, correlation Correlation, request NFTRollbackRequest) (*NFTResult, error) {
	path, err := e.root.Resolve(request.RollbackArtifactPath, true)
	if err != nil {
		return nil, err
	}
	artifact, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	shaPath, err := e.root.Resolve(request.RollbackArtifactPath+".sha256", true)
	if err != nil {
		return nil, err
	}
	recordedSHA, err := os.ReadFile(shaPath)
	if err != nil {
		return nil, err
	}
	expectedSHA := strings.TrimSpace(string(recordedSHA))
	if !validSHA256(expectedSHA) || (request.ExpectedSHA256 != "" && request.ExpectedSHA256 != expectedSHA) || sha256Hex(artifact) != expectedSHA {
		return nil, errors.New("rollback artifact SHA-256 mismatch")
	}
	if err := validateManagedScope(artifact, true); err != nil {
		return nil, err
	}
	current, present, err := e.executor.ListManagedTable(ctx)
	if err != nil {
		return nil, err
	}
	desiredAbsent := strings.TrimSpace(string(artifact)) == "delete table inet solovey_protection"
	expectedRevision := ""
	if !desiredAbsent {
		expectedRevision, err = managedRevision(artifact)
		if err != nil {
			return nil, fmt.Errorf("rollback artifact has no restorable managed revision: %w", err)
		}
	}
	if !present {
		if desiredAbsent {
			return &NFTResult{ManagedTablePresent: false, RollbackSHA256: expectedSHA}, nil
		}
		return nil, errors.New("managed table is absent before rollback")
	}
	currentRevision, err := managedRevision(current)
	if err != nil {
		return nil, fmt.Errorf("current managed table is not safely replaceable during rollback: %w", err)
	}
	if !desiredAbsent && currentRevision == expectedRevision {
		return &NFTResult{ManagedTablePresent: true, RollbackSHA256: expectedSHA}, nil
	}
	if currentRevision != request.ExpectedCurrentRevision {
		return nil, errors.New("managed table current revision does not match rollback fence")
	}
	transaction := append([]byte(nil), artifact...)
	if present && !desiredAbsent {
		transaction = append([]byte("delete table inet solovey_protection\n"), transaction...)
	}
	transactionPath, err := writeManagedAtomic(e.root, "operations/"+correlation.OperationID+"/rollback-transaction.nft", transaction)
	if err != nil {
		return nil, err
	}
	if err := e.executor.CheckManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	if err := e.executor.ApplyManagedFile(ctx, transactionPath); err != nil {
		return nil, err
	}
	after, afterPresent, err := e.executor.ListManagedTable(ctx)
	if err != nil || afterPresent == desiredAbsent {
		return nil, errors.Join(errors.New("managed table rollback verification failed"), err)
	}
	if afterPresent {
		if err := verifyManagedRevision(after, expectedRevision); err != nil {
			return nil, errors.New("managed table rollback revision verification failed")
		}
	}
	return &NFTResult{ManagedTablePresent: afterPresent, RollbackSHA256: expectedSHA}, nil
}

func DefaultCapabilities() *CapabilitiesResult {
	capabilities := make([]Capability, 0, len(allowedOperations))
	for _, operation := range []Operation{OperationCapabilities, OperationNFTValidate, OperationNFTApply, OperationNFTRollback, OperationNginxDetectVersion, OperationNginxValidate, OperationNginxInstall, OperationNginxSwitch, OperationNginxReload, OperationNginxVerify, OperationNginxRestore, OperationListenerOwnerObserve, OperationSSHRecoveryObserve, OperationArtifact} {
		available := operation == OperationCapabilities
		reason := "missing_capability"
		if available {
			reason = ""
		}
		capabilities = append(capabilities, Capability{Operation: operation, Available: available, Reason: reason})
	}
	result := &CapabilitiesResult{ProtocolVersions: []int{ProtocolVersion}, HelperVersion: HelperVersion, ContractVersion: HelperContractVersion, Capabilities: capabilities, NFT: NFTSupport{Reason: "nft_capability_unknown"}, Nginx: NginxSupport{Reason: "nginx_capability_unknown"}, SSHRecovery: SSHRecoverySupport{Reason: "ssh_recovery_capability_unknown"}, ListenerOwner: ListenerOwnerSupport{Reason: "listener_owner_capability_unknown"}}
	setCapabilityRevision(result)
	return result
}

func setCapabilityRevision(result *CapabilitiesResult) {
	if result == nil {
		return
	}
	copy := *result
	copy.Revision = ""
	data, _ := json.Marshal(copy)
	result.Revision = sha256Hex(data)
}

func CapabilityAvailable(result *CapabilitiesResult, operation Operation) bool {
	if result == nil {
		return false
	}
	for _, capability := range result.Capabilities {
		if capability.Operation == operation {
			return capability.Available
		}
	}
	return false
}

func validateNegotiation(result *CapabilitiesResult) error {
	if result == nil || result.HelperVersion == "" {
		return fmt.Errorf("helper did not return capabilities")
	}
	if result.ContractVersion != HelperContractVersion || !compatibleHelperVersion(result.HelperVersion) {
		return fmt.Errorf("helper_version_mismatch")
	}
	expectedRevision := *result
	setCapabilityRevision(&expectedRevision)
	if !validRevision(result.Revision) || result.Revision != expectedRevision.Revision {
		return fmt.Errorf("helper_version_mismatch")
	}
	found := false
	for _, version := range result.ProtocolVersions {
		if version == ProtocolVersion {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("helper_version_mismatch")
	}
	return nil
}

// ValidateCapabilities exposes the same strict protocol/contract/revision
// negotiation used by the panel client to bounded diagnostic consumers.
func ValidateCapabilities(result *CapabilitiesResult) error {
	return validateNegotiation(result)
}

func compatibleHelperVersion(version string) bool {
	parts := strings.Split(version, ".")
	return len(parts) == 3 && parts[0]+"."+parts[1] == HelperContractVersion
}
