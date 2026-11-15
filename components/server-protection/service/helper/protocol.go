// Package helper defines the versioned, typed trust boundary between the
// server-protection component and its optional privileged helper process.
// It intentionally has no HTTP types and exposes no command-string API.
package helper

import canonical "github.com/MalenkiySolovey/solovey-component-server-protection/service/helpercontract"

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
)

const (
	ProtocolVersion       = canonical.ProtocolVersion
	HelperVersion         = canonical.HelperVersion
	HelperContractVersion = canonical.HelperContractVersion
	MaxRequestBytes       = canonical.MaxRequestBytes
	// An accepted candidate must always fit through a subsequent bounded live
	// observation. nft can add formatting and volatile counters to list output,
	// so acceptance is deliberately below the transport/artifact ceilings.
	MaxManagedCandidateBytes = canonical.MaxManagedCandidateBytes
	MaxOutputBytes           = canonical.MaxOutputBytes
	MaxArtifactBytes         = canonical.MaxArtifactBytes
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

type Operation = canonical.Operation

const (
	OperationCapabilities         = canonical.OperationCapabilities
	OperationNFTValidate          = canonical.OperationNFTValidate
	OperationNFTObserve           = canonical.OperationNFTObserve
	OperationNFTApply             = canonical.OperationNFTApply
	OperationNFTRollback          = canonical.OperationNFTRollback
	OperationNginxDetectVersion   = canonical.OperationNginxDetectVersion
	OperationNginxValidate        = canonical.OperationNginxValidate
	OperationNginxInstall         = canonical.OperationNginxInstall
	OperationNginxSwitch          = canonical.OperationNginxSwitch
	OperationNginxReload          = canonical.OperationNginxReload
	OperationNginxVerify          = canonical.OperationNginxVerify
	OperationNginxRestore         = canonical.OperationNginxRestore
	OperationListenerOwnerObserve = canonical.OperationListenerOwnerObserve
	OperationSSHRecoveryObserve   = canonical.OperationSSHRecoveryObserve
)

type ErrorCode = canonical.ErrorCode

const (
	CodeInvalidRequest    = canonical.CodeInvalidRequest
	CodeUnsupported       = canonical.CodeUnsupported
	CodeMissingCapability = canonical.CodeMissingCapability
	CodePathForbidden     = canonical.CodePathForbidden
	CodeValidationFailed  = canonical.CodeValidationFailed
	CodeTimeout           = canonical.CodeTimeout
	CodeCanceled          = canonical.CodeCanceled
	CodeTransportFailed   = canonical.CodeTransportFailed
	CodeInternal          = canonical.CodeInternal
)

type Request = canonical.Request

type CapabilitiesRequest = canonical.CapabilitiesRequest

type NginxDetectVersionRequest = canonical.NginxDetectVersionRequest

type NginxValidateRequest = canonical.NginxValidateRequest

type BinaryIdentity = canonical.BinaryIdentity

type NginxInstallRequest = canonical.NginxInstallRequest

type NginxSwitchRequest = canonical.NginxSwitchRequest

type NginxReloadRequest = canonical.NginxReloadRequest

type NginxVerifyRequest = canonical.NginxVerifyRequest

type NginxRestoreRequest = canonical.NginxRestoreRequest

type NginxListener = canonical.NginxListener

type ListenerOwnerObserveRequest = canonical.ListenerOwnerObserveRequest

type SSHRecoveryObserveRequest = canonical.SSHRecoveryObserveRequest

type Capability = canonical.Capability

type CapabilitiesResult = canonical.CapabilitiesResult

type ListenerOwnerSupport = canonical.ListenerOwnerSupport

type ListenerOwnerObserveResult = canonical.ListenerOwnerObserveResult

type SSHRecoveryEvidenceKind = canonical.SSHRecoveryEvidenceKind

const (
	SSHRecoveryEvidenceJournald = canonical.SSHRecoveryEvidenceJournald
	SSHRecoveryEvidenceLogread  = canonical.SSHRecoveryEvidenceLogread
)

type SSHRecoverySupport = canonical.SSHRecoverySupport

type SSHRecoveryObservation = canonical.SSHRecoveryObservation

type SSHRecoveryResult = canonical.SSHRecoveryResult

type NginxVersionResult = canonical.NginxVersionResult

type NginxSupport = canonical.NginxSupport

type NginxResult = canonical.NginxResult

// NFTResult contains only integrity and managed-table facts. Raw nft output is
// deliberately never returned across the helper boundary.
type Response = canonical.Response

var correlationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func DecodeRequest(reader io.Reader) (Request, error) { return canonical.DecodeRequest(reader) }

func ValidateRequest(r Request, root ManagedRoot) error {
	if err := r.ValidateShape(); err != nil {
		return err
	}
	return validatePayload(r, root)
}

func validatePayload(r Request, root ManagedRoot) error {
	switch r.Operation {
	case OperationCapabilities, OperationNFTObserve, OperationNginxDetectVersion:
		return nil
	case OperationNFTValidate:
		return nftables.ValidateRequest(*r.NFTValidate, root)
	case OperationNFTApply:
		return nftables.ValidateApplyRequest(*r.NFTApply, root)
	case OperationNFTRollback:
		return nftables.ValidateRollbackRequest(*r.NFTRollback, root)
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

func validateNginxRevision(revision, sha string, binary BinaryIdentity) error {
	if !validRevision(revision) || !validSHA256(sha) {
		return errors.New("nginx operation requires an exact revision and SHA-256")
	}
	if !canonicalAbsolute(binary.Path) || !canonicalAbsolute(binary.TargetPath) {
		return errors.New("nginx operation requires an exact absolute binary identity")
	}
	if binary.Device == 0 || binary.Inode == 0 || binary.Size <= 0 || binary.Mode&0o111 == 0 || binary.Mode&0o022 != 0 ||
		binary.UID != 0 || binary.GID != 0 || !validSHA256(binary.Digest) {
		return errors.New("nginx operation requires a trusted executable-object identity")
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
