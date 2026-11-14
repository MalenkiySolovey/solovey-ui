// Package helpercontract owns typed protection operation declarations and
// strict envelope validation. It has no root, filesystem or executor port.
package helpercontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	hostfacts "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
	wire "github.com/MalenkiySolovey/solovey-host/network/nftcontract"
	"io"
	"regexp"
)

const (
	ProtocolVersion       = 1
	HelperVersion         = "1.11.0"
	HelperContractVersion = "1.11"
	MaxRequestBytes       = 1 << 20

	MaxManagedCandidateBytes = wire.MaxManagedCandidateBytes
	MaxOutputBytes           = wire.MaxOutputBytes
	MaxArtifactBytes         = wire.MaxArtifactBytes
)

type Operation string

const (
	OperationCapabilities         Operation = "capabilities"
	OperationNFTValidate          Operation = "nft.validate"
	OperationNFTObserve           Operation = "nft.managed_table.observe"
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
)

var allowedOperations = map[Operation]struct{}{
	OperationCapabilities: {}, OperationNFTValidate: {}, OperationNFTObserve: {}, OperationNFTApply: {},
	OperationNFTRollback: {}, OperationNginxDetectVersion: {}, OperationNginxValidate: {},
	OperationNginxInstall: {}, OperationNginxSwitch: {}, OperationNginxReload: {},
	OperationNginxVerify: {}, OperationNginxRestore: {}, OperationListenerOwnerObserve: {}, OperationSSHRecoveryObserve: {},
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

type Request struct {
	ProtocolVersion int         `json:"protocol_version"`
	Correlation     Correlation `json:"correlation"`
	Operation       Operation   `json:"operation"`

	Capabilities         *CapabilitiesRequest         `json:"capabilities,omitempty"`
	NFTValidate          *NFTValidateRequest          `json:"nft_validate,omitempty"`
	NFTObserve           *NFTObserveRequest           `json:"nft_observe,omitempty"`
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
}

type CapabilitiesRequest struct{}

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
	Size       int64  `json:"size,omitempty"`
	Mode       uint32 `json:"mode,omitempty"`
	UID        uint32 `json:"uid,omitempty"`
	GID        uint32 `json:"gid,omitempty"`
	Digest     string `json:"digest,omitempty"`
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

type SSHRecoveryEvidenceKind string

const (
	SSHRecoveryEvidenceJournald SSHRecoveryEvidenceKind = "journald"
	SSHRecoveryEvidenceLogread  SSHRecoveryEvidenceKind = "logread"
)

type SSHRecoverySupport struct {
	PlatformKnown    bool                    `json:"platform_known"`
	Linux            bool                    `json:"linux"`
	Available        bool                    `json:"available"`
	Reason           string                  `json:"reason,omitempty"`
	EvidenceKind     SSHRecoveryEvidenceKind `json:"evidence_kind,omitempty"`
	VerifierRevision string                  `json:"verifier_revision,omitempty"`
	ObserverRevision string                  `json:"observer_revision"`
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
	ObserverRevision string                   `json:"observer_revision"`
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

type Response struct {
	NetworkReason   string      `json:"network_reason,omitempty"`
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

func (r Request) ValidateShape() error {
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
	return nil

}

func (r Request) UnlockedReadOnly() bool {
	return r.Operation == OperationCapabilities || r.Operation == OperationNFTObserve || r.Operation == OperationSSHRecoveryObserve || r.Operation == OperationListenerOwnerObserve
}

func (r Request) validatePayloadShape() error {
	present := 0
	for _, value := range []any{r.Capabilities, r.NFTValidate, r.NFTObserve, r.NFTApply, r.NFTRollback, r.NginxDetectVersion, r.NginxValidate, r.NginxInstall, r.NginxSwitch, r.NginxReload, r.NginxVerify, r.NginxRestore, r.ListenerOwnerObserve, r.SSHRecoveryObserve} {
		if !isNilPayload(value) {
			present++
		}
	}
	if present != 1 {
		return errors.New("exactly one typed operation payload is required")
	}
	valid := (r.Operation == OperationCapabilities && r.Capabilities != nil) ||
		(r.Operation == OperationNFTValidate && r.NFTValidate != nil) ||
		(r.Operation == OperationNFTObserve && r.NFTObserve != nil) ||
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
		(r.Operation == OperationSSHRecoveryObserve && r.SSHRecoveryObserve != nil)
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
	case *NFTObserveRequest:
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
	default:
		return true
	}
}

func (r Request) RequiredLockKind() (string, error) {
	switch r.Operation {
	case OperationCapabilities, OperationNFTObserve, OperationSSHRecoveryObserve, OperationListenerOwnerObserve:
		return "", nil
	case OperationNFTValidate, OperationNFTApply, OperationNFTRollback:
		return "firewall", nil
	case OperationNginxDetectVersion, OperationNginxValidate, OperationNginxInstall, OperationNginxSwitch, OperationNginxReload, OperationNginxVerify, OperationNginxRestore:
		return "fronting", nil
	default:
		return "", fmt.Errorf("operation %q is not allowlisted", r.Operation)
	}
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

type Correlation = wire.Correlation
type NFTValidateRequest = wire.NFTValidateRequest
type NFTObserveRequest = wire.NFTObserveRequest
type NFTApplyRequest = wire.NFTApplyRequest
type NFTRollbackRequest = wire.NFTRollbackRequest
type NFTResult = wire.NFTResult
type NFTSupport = wire.NFTSupport
