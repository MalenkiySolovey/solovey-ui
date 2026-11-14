package helpercontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func ValidSSHRecoverySupport(value SSHRecoverySupport) bool {
	return value.PlatformKnown && value.Linux && value.Available && validSHA256(value.VerifierRevision) &&
		validSHA256(value.ObserverRevision) &&
		(value.EvidenceKind == SSHRecoveryEvidenceJournald || value.EvidenceKind == SSHRecoveryEvidenceLogread)
}

func SSHRecoveryCapabilityAvailable(result *CapabilitiesResult) bool {
	return result != nil && CapabilityAvailable(result, OperationSSHRecoveryObserve) && ValidSSHRecoverySupport(result.SSHRecovery)
}

func CapabilityReason(result *CapabilitiesResult, operation Operation) string {
	for _, capability := range result.Capabilities {
		if capability.Operation == operation && capability.Reason != "" {
			return capability.Reason
		}
	}
	return "missing_capability"
}

func DefaultCapabilities() *CapabilitiesResult {
	capabilities := make([]Capability, 0, len(allowedOperations))
	for _, operation := range []Operation{OperationCapabilities, OperationNFTValidate, OperationNFTObserve, OperationNFTApply, OperationNFTRollback, OperationNginxDetectVersion, OperationNginxValidate, OperationNginxInstall, OperationNginxSwitch, OperationNginxReload, OperationNginxVerify, OperationNginxRestore, OperationListenerOwnerObserve, OperationSSHRecoveryObserve} {
		available := operation == OperationCapabilities
		reason := "missing_capability"
		if available {
			reason = ""
		}
		capabilities = append(capabilities, Capability{Operation: operation, Available: available, Reason: reason})
	}
	result := &CapabilitiesResult{ProtocolVersions: []int{ProtocolVersion}, HelperVersion: HelperVersion, ContractVersion: HelperContractVersion, Capabilities: capabilities, NFT: NFTSupport{Reason: "nft_capability_unknown"}, Nginx: NginxSupport{Reason: "nginx_capability_unknown"}, SSHRecovery: SSHRecoverySupport{Reason: "ssh_recovery_capability_unknown"}, ListenerOwner: ListenerOwnerSupport{Reason: "listener_owner_capability_unknown"}}
	SetCapabilityRevision(result)
	return result
}

func SetCapabilityRevision(result *CapabilitiesResult) {
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
	if result.ContractVersion != HelperContractVersion || !CompatibleHelperVersion(result.HelperVersion) {
		return fmt.Errorf("helper_version_mismatch")
	}
	expectedRevision := *result
	SetCapabilityRevision(&expectedRevision)
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
	recoveryAdvertised := CapabilityAvailable(result, OperationSSHRecoveryObserve)
	if recoveryAdvertised != result.SSHRecovery.Available || recoveryAdvertised && !ValidSSHRecoverySupport(result.SSHRecovery) {
		return fmt.Errorf("helper_version_mismatch")
	}
	return nil
}

// ValidateCapabilities exposes the same strict protocol/contract/revision
// negotiation used by the panel client to bounded diagnostic consumers.
func ValidateCapabilities(result *CapabilitiesResult) error {
	return validateNegotiation(result)
}

func CompatibleHelperVersion(version string) bool {
	parts := strings.Split(version, ".")
	return len(parts) == 3 && parts[0]+"."+parts[1] == HelperContractVersion
}

var revisionPattern = regexp.MustCompile(`^[a-f0-9]{16,128}$`)
var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validRevision(value string) bool { return revisionPattern.MatchString(value) }
func validSHA256(value string) bool   { return sha256Pattern.MatchString(value) }
func sha256Hex(data []byte) string    { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
