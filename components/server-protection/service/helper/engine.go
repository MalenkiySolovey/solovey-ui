package helper

import (
	"context"
	"errors"
	canonical "github.com/MalenkiySolovey/solovey-component-server-protection/service/helpercontract"
	"os"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	networkplan "github.com/MalenkiySolovey/solovey-ui/network/planner"

	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
)

// ContractEngine is the privileged side of the typed protocol. Its executor
// vocabulary can address only the Solovey-managed nft table.
type ContractEngine struct {
	root                  ManagedRoot
	executor              NFTExecutor
	network               *nftables.Engine
	nginxExecutor         NginxExecutor
	listenerOwnerExecutor ListenerOwnerExecutor
	sshRecoveryExecutor   SSHRecoveryExecutor
	now                   func() time.Time
}

func NewContractEngine(root ManagedRoot) ContractEngine {
	return newContractEngine(root, sshbroker.ResolvedSSHComposition{})
}

// NewContractEngineWithSSHComposition accepts only the resolved release
// record produced by the SSH composition authority. Recovery never parses
// deployment environment selectors or caller-supplied targets.
func NewContractEngineWithSSHComposition(root ManagedRoot, composition sshbroker.ResolvedSSHComposition) ContractEngine {
	return newContractEngine(root, composition)
}

func newContractEngine(root ManagedRoot, composition sshbroker.ResolvedSSHComposition) ContractEngine {
	network := nftables.NewEngine(root)
	return ContractEngine{root: root, network: &network, nginxExecutor: newSystemNginxExecutor(root), listenerOwnerExecutor: newComposedListenerOwnerExecutor(), sshRecoveryExecutor: newComposedSSHRecoveryExecutor(composition), now: time.Now}
}

func (e ContractEngine) networkEngine() nftables.Engine {
	if e.network != nil {
		return *e.network
	}
	return nftables.NewEngineWithExecutor(e.root, e.executor, e.now)
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
	if err := ValidateRequest(request, e.root); err != nil {
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
		response.NFT, err = e.networkEngine().Validate(ctx, request.Correlation, *request.NFTValidate)
	case OperationNFTObserve:
		response.NFT, err = e.networkEngine().Observe(ctx)
	case OperationNFTApply:
		response.NFT, err = e.networkEngine().Apply(ctx, request.Correlation, *request.NFTApply)
	case OperationNFTRollback:
		response.NFT, err = e.networkEngine().Rollback(ctx, request.Correlation, *request.NFTRollback)
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
		var conflict *networkplan.Conflict
		if errors.As(err, &conflict) {
			response.NetworkReason = string(conflict.Reason)
		}
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
	result.NFT = e.networkEngine().Support(ctx)
	if result.NFT.PlatformKnown && result.NFT.Linux && result.NFT.Available {
		for index := range result.Capabilities {
			switch result.Capabilities[index].Operation {
			case OperationNFTValidate, OperationNFTObserve, OperationNFTApply, OperationNFTRollback:
				result.Capabilities[index].Available = true
				result.Capabilities[index].Reason = ""
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
		if validSSHRecoverySupport(result.SSHRecovery) {
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

func validSSHRecoverySupport(value SSHRecoverySupport) bool {
	return canonical.ValidSSHRecoverySupport(value)
}
func SSHRecoveryCapabilityAvailable(value *CapabilitiesResult) bool {
	return canonical.SSHRecoveryCapabilityAvailable(value)
}
func capabilityReason(value *CapabilitiesResult, operation Operation) string {
	return canonical.CapabilityReason(value, operation)
}
func DefaultCapabilities() *CapabilitiesResult        { return canonical.DefaultCapabilities() }
func setCapabilityRevision(value *CapabilitiesResult) { canonical.SetCapabilityRevision(value) }
func CapabilityAvailable(value *CapabilitiesResult, operation Operation) bool {
	return canonical.CapabilityAvailable(value, operation)
}
func validateNegotiation(value *CapabilitiesResult) error {
	return canonical.ValidateCapabilities(value)
}
func ValidateCapabilities(value *CapabilitiesResult) error {
	return canonical.ValidateCapabilities(value)
}
func compatibleHelperVersion(value string) bool { return canonical.CompatibleHelperVersion(value) }
