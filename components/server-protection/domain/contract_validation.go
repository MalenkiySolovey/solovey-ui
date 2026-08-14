package domain

import (
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

const (
	maxContractIDBytes = 256
	maxReasonCodes     = 32
	maxReasonCodeBytes = 64
)

// ValidContractID accepts opaque, bounded identifiers only. Paths, query
// fragments, serialized payloads, and whitespace are deliberately excluded.
func ValidContractID(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if limit <= 0 || limit > maxContractIDBytes {
		limit = maxContractIDBytes
	}
	if value == "" || len(value) > limit || !utf8.ValidString(value) || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func ValidSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func ValidExactRevision(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validateReasonCodes(values []string) bool {
	if len(values) > maxReasonCodes {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !ValidContractID(value, maxReasonCodeBytes) {
			return false
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func SignalScopeAllowed(category SignalCategory, scope DecisionScope) bool {
	switch category {
	case SignalCategoryEndpointObservation, SignalCategoryConnectionMetadata, SignalCategoryConfigDrift:
		return scope == ScopeEndpoint || scope == ScopeService
	case SignalCategoryExternalReputation:
		return scope == ScopeEndpoint
	case SignalCategoryPanelAuth:
		return scope == ScopePanelAuth
	case SignalCategoryPanelAPI:
		return scope == ScopePanelAPI
	case SignalCategorySubscription:
		return scope == ScopeSubscription
	case SignalCategorySSHAuth:
		return scope == ScopeSSH
	case SignalCategoryKernelPressure:
		return scope == ScopeEndpoint
	case SignalCategoryHostSurface, SignalCategoryHostResource:
		return scope == ScopeHostWide
	default:
		return false
	}
}

// SignalKindKnown is the contract v2 versioned enum boundary. New producer kinds
// remain storable only with KnownKind=false until the contract enum is
// deliberately extended.
func SignalKindKnown(category SignalCategory, kind string) bool {
	value := SignalKind(strings.TrimSpace(kind))
	if value.Validate() != nil {
		return false
	}
	if category == SignalCategoryConfigDrift {
		return value == SignalResourceDrift
	}
	switch category {
	case SignalCategoryEndpointObservation:
		return value == SignalHTTPScannerPath || value == SignalHTTPEmptyUA || value == SignalHTTPScannerUA ||
			value == SignalHTTPUnexpectedMethod || value == SignalRateLimited
	case SignalCategoryConnectionMetadata:
		return value == SignalMissingSNI || value == SignalUnexpectedALPN || value == SignalFallbackHit ||
			value == SignalShortTLSSession || value == SignalTinyTransfer || value == SignalRealClientCorrelation
	case SignalCategoryKernelPressure:
		return value == SignalSYNBurst || value == SignalConntrackPressure
	case SignalCategoryExternalReputation:
		return value == SignalExternalReputation
	default:
		return false
	}
}

func validateScopeShape(value SignalScopeV2) bool {
	if !validScope(value.Scope) {
		return false
	}
	target := strings.TrimSpace(value.TargetResourceID)
	switch value.Scope {
	case ScopeEndpoint, ScopeService:
		if !ValidContractID(target, maxContractIDBytes) {
			return false
		}
		if value.EndpointID != "" && !ValidContractID(value.EndpointID, maxContractIDBytes) {
			return false
		}
		if value.Transport != "" && value.Transport != "tcp" && value.Transport != "udp" {
			return false
		}
		return true
	default:
		// Semantic management/account scopes cannot name an opaque resource until
		// a trusted scope-to-target mapping (firewall baseline) exists.
		return target == "" && value.EndpointID == "" && value.Transport == ""
	}
}

func validatePolicyCheck(value PolicyCheckV2) bool {
	switch value.Result {
	case "allow", "deny", "unknown", "not_evaluated", "stale", "ambiguous":
	default:
		return false
	}
	return validateReasonCodes(value.ReasonCodes)
}
