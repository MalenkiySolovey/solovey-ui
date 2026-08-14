// Package helper defines the versioned, typed trust boundary between the
// server-protection component and its optional privileged helper process.
// It intentionally has no HTTP types and exposes no command-string API.
package helper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

const (
	ProtocolVersion       = 1
	HelperVersion         = "1.5.1"
	HelperContractVersion = "1.5"
	MaxRequestBytes       = 1 << 20
	MaxOutputBytes        = 256 << 10
	MaxArtifactBytes      = 512 << 10
)

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || original > 0
		return original, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(value)
	return original, nil
}

type Operation string

const (
	OperationCapabilities         Operation = "capabilities"
	OperationNFTValidate          Operation = "nft.validate"
	OperationNFTApply             Operation = "nft.managed_table.apply"
	OperationNFTRollback          Operation = "nft.managed_table.rollback"
	OperationNginxDetectVersion   Operation = "nginx.detect_version"
	OperationNginxValidate        Operation = "nginx.config.validate"
	OperationNginxInstall         Operation = "nginx.revision.install"
	OperationNginxSwitch          Operation = "nginx.active.switch"
	OperationNginxReload          Operation = "nginx.reload"
	OperationNginxVerify          Operation = "nginx.active.verify"
	OperationNginxRestore         Operation = "nginx.revision.restore"
	OperationListenerOwnerObserve Operation = "listener.owner.observe"
	OperationSSHRecoveryObserve   Operation = "recovery.ssh.observe"
	OperationArtifact             Operation = "artifact.manage"
)

var allowedOperations = map[Operation]struct{}{
	OperationCapabilities: {}, OperationNFTValidate: {}, OperationNFTApply: {},
	OperationNFTRollback: {}, OperationNginxDetectVersion: {}, OperationNginxValidate: {},
	OperationNginxInstall: {}, OperationNginxSwitch: {}, OperationNginxReload: {},
	OperationNginxVerify: {}, OperationNginxRestore: {}, OperationListenerOwnerObserve: {}, OperationSSHRecoveryObserve: {}, OperationArtifact: {},
}

type ErrorCode string

const (
	CodeInvalidRequest    ErrorCode = "invalid_request"
	CodeUnsupported       ErrorCode = "unsupported_operation"
	CodeMissingCapability ErrorCode = "missing_capability"
	CodePathForbidden     ErrorCode = "path_forbidden"
	CodeValidationFailed  ErrorCode = "validation_failed"
	CodeTimeout           ErrorCode = "timeout"
	CodeCanceled          ErrorCode = "canceled"
	CodeTransportFailed   ErrorCode = "transport_failed"
	CodeInternal          ErrorCode = "internal_error"
)

type Correlation struct {
	OperationID  string `json:"operation_id"`
	InstanceID   string `json:"instance_id"`
	LockRevision int    `json:"lock_revision"`
}

type Request struct {
	ProtocolVersion int         `json:"protocol_version"`
	Correlation     Correlation `json:"correlation"`
	Operation       Operation   `json:"operation"`

	Capabilities         *CapabilitiesRequest         `json:"capabilities,omitempty"`
	NFTValidate          *NFTValidateRequest          `json:"nft_validate,omitempty"`
	NFTApply             *NFTApplyRequest             `json:"nft_apply,omitempty"`
	NFTRollback          *NFTRollbackRequest          `json:"nft_rollback,omitempty"`
	NginxDetectVersion   *NginxDetectVersionRequest   `json:"nginx_detect_version,omitempty"`
	NginxValidate        *NginxValidateRequest        `json:"nginx_validate,omitempty"`
	NginxInstall         *NginxInstallRequest         `json:"nginx_install,omitempty"`
	NginxSwitch          *NginxSwitchRequest          `json:"nginx_switch,omitempty"`
	NginxReload          *NginxReloadRequest          `json:"nginx_reload,omitempty"`
	NginxVerify          *NginxVerifyRequest          `json:"nginx_verify,omitempty"`
	NginxRestore         *NginxRestoreRequest         `json:"nginx_restore,omitempty"`
	ListenerOwnerObserve *ListenerOwnerObserveRequest `json:"listener_owner_observe,omitempty"`
	SSHRecoveryObserve   *SSHRecoveryObserveRequest   `json:"ssh_recovery_observe,omitempty"`
	Artifact             *ArtifactRequest             `json:"artifact,omitempty"`
}

type CapabilitiesRequest struct{}

type NFTValidateRequest struct {
	CandidatePath    string `json:"candidate_path"`
	ExpectedRevision string `json:"expected_revision"`
	ExpectedSHA256   string `json:"expected_sha256"`
}

type NFTApplyRequest struct {
	CandidatePath                string `json:"candidate_path"`
	RollbackArtifactPath         string `json:"rollback_artifact_path"`
	ExpectedTable                string `json:"expected_table"`
	ExpectedRevision             string `json:"expected_revision"`
	ExpectedSHA256               string `json:"expected_sha256"`
	ExpectedPreviousRevision     string `json:"expected_previous_revision,omitempty"`
	ExpectedPreviousSHA256       string `json:"expected_previous_sha256,omitempty"`
	ExpectedPreviousTablePresent bool   `json:"expected_previous_table_present"`
}

type NFTRollbackRequest struct {
	RollbackArtifactPath    string `json:"rollback_artifact_path"`
	ExpectedTable           string `json:"expected_table"`
	ExpectedSHA256          string `json:"expected_sha256"`
	ExpectedCurrentRevision string `json:"expected_current_revision"`
}

type NginxDetectVersionRequest struct{}

type NginxValidateRequest struct {
	CandidatePath    string         `json:"candidate_path"`
	ExpectedRevision string         `json:"expected_revision"`
	ExpectedSHA256   string         `json:"expected_sha256"`
	ExpectedBinary   BinaryIdentity `json:"expected_binary"`
}

type BinaryIdentity struct {
	Path       string `json:"path"`
	TargetPath string `json:"target_path"`
	Device     uint64 `json:"device,omitempty"`
	Inode      uint64 `json:"inode,omitempty"`
}

type NginxInstallRequest struct {
	CandidatePath    string          `json:"candidate_path"`
	ExpectedRevision string          `json:"expected_revision"`
	ExpectedSHA256   string          `json:"expected_sha256"`
	Listeners        []NginxListener `json:"listeners"`
}

type NginxSwitchRequest struct {
	ExpectedPreviousRevision string `json:"expected_previous_revision,omitempty"`
	TargetRevision           string `json:"target_revision"`
	ExpectedSHA256           string `json:"expected_sha256"`
}

type NginxReloadRequest struct {
	ExpectedRevision string         `json:"expected_revision"`
	ExpectedSHA256   string         `json:"expected_sha256"`
	ExpectedBinary   BinaryIdentity `json:"expected_binary"`
}

type NginxVerifyRequest struct {
	ExpectedRevision string          `json:"expected_revision"`
	ExpectedSHA256   string          `json:"expected_sha256"`
	ExpectedBinary   BinaryIdentity  `json:"expected_binary"`
	Listeners        []NginxListener `json:"listeners"`
}

type NginxRestoreRequest struct {
	ExpectedCurrentRevision string `json:"expected_current_revision"`
	PreviousRevision        string `json:"previous_revision"`
	ExpectedSHA256          string `json:"expected_sha256"`
}

type NginxListener struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type ListenerOwnerObserveRequest struct {
	ResourceID                         string `json:"resource_id"`
	Network                            string `json:"network"`
	ConfiguredMode                     string `json:"configured_mode"`
	ConfiguredAddress                  string `json:"configured_address"`
	Port                               int    `json:"port"`
	ExpectedInstanceID                 string `json:"expected_instance_id"`
	ExpectedSourceRevision             string `json:"expected_source_revision"`
	ExpectedArtifactRevision           string `json:"expected_artifact_revision"`
	ExpectedDeploymentID               string `json:"expected_deployment_id"`
	ExpectedOwnerContractRevision      string `json:"expected_owner_contract_revision"`
	ExpectedRuntimeRootBindingRevision string `json:"expected_runtime_root_binding_revision"`
	ExpectedResourceOwnerRevision      string `json:"expected_resource_owner_revision"`
	ExpectedConfigurationRevision      string `json:"expected_configuration_revision"`
}

type SSHRecoveryObserveRequest struct {
	SinceUnixMicros int64 `json:"since_unix_micros"`
	MaxEvents       int   `json:"max_events"`
}

type ArtifactScope string

const (
	ArtifactScopeNFT   ArtifactScope = "nft"
	ArtifactScopeNginx ArtifactScope = "nginx"
)

type ArtifactAction string

const (
	ArtifactInspect       ArtifactAction = "inspect"
	ArtifactWriteAtomic   ArtifactAction = "write_atomic"
	ArtifactReplaceAtomic ArtifactAction = "replace_atomic"
	ArtifactRemove        ArtifactAction = "remove"
)

type ArtifactRequest struct {
	Scope       ArtifactScope  `json:"scope"`
	Action      ArtifactAction `json:"action"`
	Path        string         `json:"path"`
	SourcePath  string         `json:"source_path,omitempty"`
	Content     []byte         `json:"content,omitempty"`
	Permissions string         `json:"permissions,omitempty"`
}

type Capability struct {
	Operation Operation `json:"operation"`
	Available bool      `json:"available"`
	Reason    string    `json:"reason,omitempty"`
}

type CapabilitiesResult struct {
	ProtocolVersions []int                `json:"protocol_versions"`
	HelperVersion    string               `json:"helper_version"`
	ContractVersion  string               `json:"contract_version"`
	Capabilities     []Capability         `json:"capabilities"`
	NFT              NFTSupport           `json:"nft"`
	Nginx            NginxSupport         `json:"nginx"`
	SSHRecovery      SSHRecoverySupport   `json:"ssh_recovery"`
	ListenerOwner    ListenerOwnerSupport `json:"listener_owner"`
	Revision         string               `json:"revision"`
}

type ListenerOwnerSupport struct {
	PlatformKnown    bool   `json:"platform_known"`
	Linux            bool   `json:"linux"`
	Available        bool   `json:"available"`
	Reason           string `json:"reason,omitempty"`
	ContractRevision string `json:"contract_revision,omitempty"`
	ObserverRevision string `json:"observer_revision,omitempty"`
}

type ListenerOwnerObserveResult struct {
	Facts               []hostfacts.ListenerOwnerFactV1 `json:"facts"`
	ReasonCodes         []string                        `json:"reason_codes,omitempty"`
	ObservationRevision string                          `json:"observation_revision"`
}

type SSHRecoverySupport struct {
	PlatformKnown    bool   `json:"platform_known"`
	Linux            bool   `json:"linux"`
	Available        bool   `json:"available"`
	Reason           string `json:"reason,omitempty"`
	JournalBinary    string `json:"journal_binary,omitempty"`
	VerifierRevision string `json:"verifier_revision,omitempty"`
}

type SSHRecoveryObservation struct {
	ObservationID       string `json:"observation_id"`
	PrincipalID         string `json:"principal_id"`
	SourcePrefix        string `json:"source_prefix"`
	AuthenticationClass string `json:"authentication_class"`
	ObservedAt          int64  `json:"observed_at"`
	ObservedAtMicros    int64  `json:"observed_at_micros"`
}

type SSHRecoveryResult struct {
	VerifierRevision string                   `json:"verifier_revision"`
	Observations     []SSHRecoveryObservation `json:"observations"`
}

type NginxVersionResult struct {
	Detected bool           `json:"detected"`
	Version  string         `json:"version,omitempty"`
	Modules  []string       `json:"modules,omitempty"`
	Binary   BinaryIdentity `json:"binary"`
}

type NginxSupport struct {
	PlatformKnown    bool            `json:"platform_known"`
	Linux            bool            `json:"linux"`
	Available        bool            `json:"available"`
	Reason           string          `json:"reason,omitempty"`
	Binary           BinaryIdentity  `json:"binary"`
	ManagedRoot      string          `json:"managed_root,omitempty"`
	ControlledConfig string          `json:"controlled_config,omitempty"`
	ActiveRevision   string          `json:"active_revision,omitempty"`
	ActiveSHA256     string          `json:"active_sha256,omitempty"`
	MasterPID        int             `json:"master_pid,omitempty"`
	Listeners        []NginxListener `json:"listeners,omitempty"`
}

type NginxResult struct {
	Revision         string         `json:"revision,omitempty"`
	SHA256           string         `json:"sha256,omitempty"`
	PreviousRevision string         `json:"previous_revision,omitempty"`
	PreviousSHA256   string         `json:"previous_sha256,omitempty"`
	Binary           BinaryIdentity `json:"binary"`
	MasterPID        int            `json:"master_pid,omitempty"`
	WorkerPIDs       []int          `json:"worker_pids,omitempty"`
	ListenersMatched bool           `json:"listeners_matched"`
	Diagnostics      []string       `json:"diagnostics,omitempty"`
}

type ArtifactResult struct {
	Paths []string `json:"paths,omitempty"`
}

// NFTResult contains only integrity and managed-table facts. Raw nft output is
// deliberately never returned across the helper boundary.
type NFTResult struct {
	ManagedTablePresent  bool   `json:"managed_table_present"`
	AppliedRevision      string `json:"applied_revision,omitempty"`
	CandidateSHA256      string `json:"candidate_sha256,omitempty"`
	RollbackSHA256       string `json:"rollback_sha256,omitempty"`
	PreviousRevision     string `json:"previous_revision,omitempty"`
	PreviousSHA256       string `json:"previous_sha256,omitempty"`
	PreviousTablePresent bool   `json:"previous_table_present"`
}

type Response struct {
	ProtocolVersion int         `json:"protocol_version"`
	HelperVersion   string      `json:"helper_version"`
	Correlation     Correlation `json:"correlation"`
	Operation       Operation   `json:"operation"`
	OK              bool        `json:"ok"`
	Code            ErrorCode   `json:"code,omitempty"`
	Reason          string      `json:"reason,omitempty"`
	Warnings        []string    `json:"warnings,omitempty"`

	Capabilities  *CapabilitiesResult         `json:"capabilities,omitempty"`
	NginxVersion  *NginxVersionResult         `json:"nginx_version,omitempty"`
	Nginx         *NginxResult                `json:"nginx,omitempty"`
	ListenerOwner *ListenerOwnerObserveResult `json:"listener_owner,omitempty"`
	SSHRecovery   *SSHRecoveryResult          `json:"ssh_recovery,omitempty"`
	Artifact      *ArtifactResult             `json:"artifact,omitempty"`
	NFT           *NFTResult                  `json:"nft,omitempty"`
}

var correlationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func DecodeRequest(reader io.Reader) (Request, error) {
	data, err := readBounded(reader, MaxRequestBytes)
	if err != nil {
		return Request{}, err
	}
	var request Request
	if err := decodeStrict(data, &request); err != nil {
		return Request{}, fmt.Errorf("decode helper request: %w", err)
	}
	return request, nil
}

func (r Request) Validate(root ManagedRoot) error {
	if r.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("protocol version %d is unsupported", r.ProtocolVersion)
	}
	if _, ok := allowedOperations[r.Operation]; !ok {
		return fmt.Errorf("operation %q is not allowlisted", r.Operation)
	}
	if !correlationPattern.MatchString(r.Correlation.OperationID) || !correlationPattern.MatchString(r.Correlation.InstanceID) {
		return errors.New("operation_id and instance_id must be bounded opaque identifiers")
	}
	if !r.UnlockedReadOnly() && r.Correlation.LockRevision <= 0 {
		return errors.New("a positive lock_revision is required")
	}
	if err := r.validatePayloadShape(); err != nil {
		return err
	}
	return r.validatePayload(root)
}

func (r Request) UnlockedReadOnly() bool {
	return r.Operation == OperationCapabilities || r.Operation == OperationSSHRecoveryObserve || r.Operation == OperationListenerOwnerObserve
}

func (r Request) validatePayloadShape() error {
	present := 0
	for _, value := range []any{r.Capabilities, r.NFTValidate, r.NFTApply, r.NFTRollback, r.NginxDetectVersion, r.NginxValidate, r.NginxInstall, r.NginxSwitch, r.NginxReload, r.NginxVerify, r.NginxRestore, r.ListenerOwnerObserve, r.SSHRecoveryObserve, r.Artifact} {
		if !isNilPayload(value) {
			present++
		}
	}
	if present != 1 {
		return errors.New("exactly one typed operation payload is required")
	}
	valid := (r.Operation == OperationCapabilities && r.Capabilities != nil) ||
		(r.Operation == OperationNFTValidate && r.NFTValidate != nil) ||
		(r.Operation == OperationNFTApply && r.NFTApply != nil) ||
		(r.Operation == OperationNFTRollback && r.NFTRollback != nil) ||
		(r.Operation == OperationNginxDetectVersion && r.NginxDetectVersion != nil) ||
		(r.Operation == OperationNginxValidate && r.NginxValidate != nil) ||
		(r.Operation == OperationNginxInstall && r.NginxInstall != nil) ||
		(r.Operation == OperationNginxSwitch && r.NginxSwitch != nil) ||
		(r.Operation == OperationNginxReload && r.NginxReload != nil) ||
		(r.Operation == OperationNginxVerify && r.NginxVerify != nil) ||
		(r.Operation == OperationNginxRestore && r.NginxRestore != nil) ||
		(r.Operation == OperationListenerOwnerObserve && r.ListenerOwnerObserve != nil) ||
		(r.Operation == OperationSSHRecoveryObserve && r.SSHRecoveryObserve != nil) ||
		(r.Operation == OperationArtifact && r.Artifact != nil)
	if !valid {
		return errors.New("operation does not match its typed payload")
	}
	return nil
}

func isNilPayload(value any) bool {
	switch v := value.(type) {
	case *CapabilitiesRequest:
		return v == nil
	case *NFTValidateRequest:
		return v == nil
	case *NFTApplyRequest:
		return v == nil
	case *NFTRollbackRequest:
		return v == nil
	case *NginxDetectVersionRequest:
		return v == nil
	case *NginxValidateRequest:
		return v == nil
	case *NginxInstallRequest:
		return v == nil
	case *NginxSwitchRequest:
		return v == nil
	case *NginxReloadRequest:
		return v == nil
	case *NginxVerifyRequest:
		return v == nil
	case *NginxRestoreRequest:
		return v == nil
	case *ListenerOwnerObserveRequest:
		return v == nil
	case *SSHRecoveryObserveRequest:
		return v == nil
	case *ArtifactRequest:
		return v == nil
	default:
		return true
	}
}

func (r Request) validatePayload(root ManagedRoot) error {
	resolve := func(path string, mustExist bool) error {
		_, err := root.Resolve(path, mustExist)
		return err
	}
	switch r.Operation {
	case OperationCapabilities, OperationNginxDetectVersion:
		return nil
	case OperationNFTValidate:
		if !validRevision(r.NFTValidate.ExpectedRevision) || !validSHA256(r.NFTValidate.ExpectedSHA256) {
			return errors.New("nft validation requires a bounded revision and SHA-256")
		}
		return resolve(r.NFTValidate.CandidatePath, true)
	case OperationNFTApply:
		if r.NFTApply.ExpectedTable != "inet solovey_protection" {
			return errors.New("nft apply is restricted to inet solovey_protection")
		}
		if err := resolve(r.NFTApply.CandidatePath, true); err != nil {
			return err
		}
		if !validRevision(r.NFTApply.ExpectedRevision) || !validSHA256(r.NFTApply.ExpectedSHA256) {
			return errors.New("nft apply requires a bounded revision and SHA-256")
		}
		if r.NFTApply.ExpectedPreviousTablePresent {
			if !validRevision(r.NFTApply.ExpectedPreviousRevision) || !validSHA256(r.NFTApply.ExpectedPreviousSHA256) {
				return errors.New("nft apply previous managed-table fence is malformed")
			}
		} else if r.NFTApply.ExpectedPreviousRevision != "" || r.NFTApply.ExpectedPreviousSHA256 != "" {
			return errors.New("nft apply absent-table fence contains a previous identity")
		}
		return resolve(r.NFTApply.RollbackArtifactPath, false)
	case OperationNFTRollback:
		if r.NFTRollback.ExpectedTable != "inet solovey_protection" {
			return errors.New("nft rollback is restricted to inet solovey_protection")
		}
		if r.NFTRollback.ExpectedSHA256 != "" && !validSHA256(r.NFTRollback.ExpectedSHA256) {
			return errors.New("nft rollback SHA-256 is malformed")
		}
		if !validRevision(r.NFTRollback.ExpectedCurrentRevision) {
			return errors.New("nft rollback current revision fence is malformed")
		}
		// Existence is checked by the privileged engine. The panel-side client
		// validates only the managed path because the helper creates this file.
		return resolve(r.NFTRollback.RollbackArtifactPath, false)
	case OperationNginxValidate:
		if err := validateNginxRevision(r.NginxValidate.ExpectedRevision, r.NginxValidate.ExpectedSHA256, r.NginxValidate.ExpectedBinary); err != nil {
			return err
		}
		_, err := root.ResolveNoSymlink(r.NginxValidate.CandidatePath, true)
		return err
	case OperationNginxInstall:
		if !validRevision(r.NginxInstall.ExpectedRevision) || !validSHA256(r.NginxInstall.ExpectedSHA256) {
			return errors.New("nginx install requires an exact revision and SHA-256")
		}
		if err := validateNginxListeners(r.NginxInstall.Listeners); err != nil {
			return err
		}
		_, err := root.ResolveNoSymlink(r.NginxInstall.CandidatePath, true)
		return err
	case OperationNginxSwitch:
		if !validRevision(r.NginxSwitch.TargetRevision) || !validSHA256(r.NginxSwitch.ExpectedSHA256) ||
			(r.NginxSwitch.ExpectedPreviousRevision != "" && !validRevision(r.NginxSwitch.ExpectedPreviousRevision)) {
			return errors.New("nginx switch requires exact managed revisions and SHA-256")
		}
		return nil
	case OperationNginxReload:
		return validateNginxRevision(r.NginxReload.ExpectedRevision, r.NginxReload.ExpectedSHA256, r.NginxReload.ExpectedBinary)
	case OperationNginxVerify:
		if err := validateNginxRevision(r.NginxVerify.ExpectedRevision, r.NginxVerify.ExpectedSHA256, r.NginxVerify.ExpectedBinary); err != nil {
			return err
		}
		return validateNginxListeners(r.NginxVerify.Listeners)
	case OperationNginxRestore:
		if !validRevision(r.NginxRestore.ExpectedCurrentRevision) || !validRevision(r.NginxRestore.PreviousRevision) || !validSHA256(r.NginxRestore.ExpectedSHA256) {
			return errors.New("nginx restore requires exact current and previous revisions")
		}
		if r.NginxRestore.ExpectedCurrentRevision == r.NginxRestore.PreviousRevision {
			return errors.New("nginx restore revisions must differ")
		}
		return nil
	case OperationListenerOwnerObserve:
		request := r.ListenerOwnerObserve
		if !correlationPattern.MatchString(request.ResourceID) || request.Network != "tcp" && request.Network != "udp" || request.Port < 1 || request.Port > 65535 {
			return errors.New("listener owner observation resource or socket is invalid")
		}
		normalized := strings.TrimSpace(request.ConfiguredAddress)
		switch request.ConfiguredMode {
		case "exact":
			address, err := netip.ParseAddr(normalized)
			if err != nil || address.IsUnspecified() || address.IsMulticast() || address.Unmap().String() != normalized {
				return errors.New("exact listener owner address is invalid")
			}
		case "wildcard":
			if normalized != "*" && normalized != "0.0.0.0" && normalized != "::" {
				return errors.New("wildcard listener owner address is invalid")
			}
		case "dual_stack":
			if normalized != "*" {
				return errors.New("dual-stack listener owner address must use the explicit wildcard contract")
			}
		default:
			return errors.New("listener owner configured mode is not allowlisted")
		}
		if !uuidValuePattern.MatchString(request.ExpectedInstanceID) || !prefixedRevision(request.ExpectedSourceRevision, "src-") || !prefixedRevision(request.ExpectedArtifactRevision, "art-") || !prefixedRevision(request.ExpectedDeploymentID, "dep-") ||
			!validSHA256(request.ExpectedOwnerContractRevision) || !validSHA256(request.ExpectedRuntimeRootBindingRevision) || !validSHA256(request.ExpectedResourceOwnerRevision) || !validSHA256(request.ExpectedConfigurationRevision) {
			return errors.New("listener owner deployment or revision fence is malformed")
		}
		return nil
	case OperationSSHRecoveryObserve:
		nowMicros := time.Now().UTC().UnixMicro()
		if r.SSHRecoveryObserve.MaxEvents < 1 || r.SSHRecoveryObserve.MaxEvents > 64 || r.SSHRecoveryObserve.SinceUnixMicros <= 0 || r.SSHRecoveryObserve.SinceUnixMicros > nowMicros+int64((5*time.Minute)/time.Microsecond) || r.SSHRecoveryObserve.SinceUnixMicros < nowMicros-int64((10*time.Minute)/time.Microsecond) {
			return errors.New("SSH recovery observation window is invalid")
		}
		return nil
	case OperationArtifact:
		if r.Artifact.Scope != ArtifactScopeNFT && r.Artifact.Scope != ArtifactScopeNginx {
			return errors.New("artifact scope is not allowlisted")
		}
		switch r.Artifact.Action {
		case ArtifactInspect, ArtifactWriteAtomic, ArtifactReplaceAtomic, ArtifactRemove:
		default:
			return errors.New("artifact action is not allowlisted")
		}
		if len(r.Artifact.Content) > MaxArtifactBytes {
			return errors.New("artifact content exceeds 512 KiB")
		}
		if r.Artifact.Permissions != "" && r.Artifact.Permissions != "0600" && r.Artifact.Permissions != "0640" {
			return errors.New("artifact permissions are not allowlisted")
		}
		if err := resolve(r.Artifact.Path, r.Artifact.Action == ArtifactInspect || r.Artifact.Action == ArtifactRemove); err != nil {
			return err
		}
		if r.Artifact.Action == ArtifactReplaceAtomic {
			if r.Artifact.SourcePath == "" {
				return errors.New("replace_atomic requires source_path")
			}
			return resolve(r.Artifact.SourcePath, true)
		}
		if r.Artifact.SourcePath != "" {
			return errors.New("source_path is valid only for replace_atomic")
		}
		return nil
	default:
		return fmt.Errorf("operation %q is not allowlisted", r.Operation)
	}
}

var revisionPattern = regexp.MustCompile(`^[a-f0-9]{16,128}$`)
var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var uuidValuePattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

func validRevision(value string) bool { return revisionPattern.MatchString(value) }
func validSHA256(value string) bool   { return sha256Pattern.MatchString(value) }
func prefixedRevision(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && validSHA256(strings.TrimPrefix(value, prefix))
}

func (r Request) RequiredLockKind() (string, error) {
	switch r.Operation {
	case OperationCapabilities, OperationSSHRecoveryObserve, OperationListenerOwnerObserve:
		return "", nil
	case OperationNFTValidate, OperationNFTApply, OperationNFTRollback:
		return "firewall", nil
	case OperationNginxDetectVersion, OperationNginxValidate, OperationNginxInstall, OperationNginxSwitch, OperationNginxReload, OperationNginxVerify, OperationNginxRestore:
		return "fronting", nil
	case OperationArtifact:
		if r.Artifact != nil && r.Artifact.Scope == ArtifactScopeNFT {
			return "firewall", nil
		}
		return "fronting", nil
	default:
		return "", fmt.Errorf("operation %q is not allowlisted", r.Operation)
	}
}

func validateNginxRevision(revision, sha string, binary BinaryIdentity) error {
	if !validRevision(revision) || !validSHA256(sha) {
		return errors.New("nginx operation requires an exact revision and SHA-256")
	}
	if !canonicalAbsolute(binary.Path) || !canonicalAbsolute(binary.TargetPath) {
		return errors.New("nginx operation requires an exact absolute binary identity")
	}
	return nil
}

func canonicalAbsolute(value string) bool {
	if filepath.IsAbs(value) {
		return filepath.Clean(value) == value
	}
	return strings.HasPrefix(value, "/") && pathpkg.Clean(value) == value
}

func validateNginxListeners(listeners []NginxListener) error {
	if len(listeners) == 0 || len(listeners) > 64 {
		return errors.New("nginx operation requires bounded exact listeners")
	}
	for _, listener := range listeners {
		address, err := netip.ParseAddr(strings.TrimSpace(listener.Address))
		if err != nil || address.IsMulticast() || listener.Port < 1 || listener.Port > 65535 {
			return errors.New("nginx listener is invalid")
		}
	}
	return nil
}

func readBounded(reader io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("input exceeds allowed size")
	}
	return data, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are forbidden")
	}
	return nil
}
