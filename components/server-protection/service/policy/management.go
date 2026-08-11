package policy

import (
	"net/netip"
	"sort"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

type ManagementGuardState string

const (
	ManagementGuardNotApplicable ManagementGuardState = "NOT_APPLICABLE"
	ManagementGuardAllowed       ManagementGuardState = "ALLOWED"
	ManagementGuardProtected     ManagementGuardState = "PROTECTED"
	ManagementGuardUnknown       ManagementGuardState = "UNKNOWN"
)

type ManagementGuardInput struct {
	Scope              domain.SignalScopeV2
	Subject            domain.SignalSubjectV2
	EndpointKey        hostresources.PublicEndpointKey
	Management         []hostresources.ManagementEndpointV1
	RecoveryPaths      []hostresources.RecoveryPathV1
	TrustedSources     []string
	MayRestrictTraffic bool
	Now                time.Time
}

type ManagementGuardResult struct {
	State                     ManagementGuardState `json:"state"`
	ProtectedEndpointIDs      []string             `json:"protectedEndpointIds"`
	FreshRecoveryPathIDs      []string             `json:"freshRecoveryPathIds"`
	UnaffectedRecoveryPathIDs []string             `json:"unaffectedRecoveryPathIds"`
	TrustedSourceMatched      bool                 `json:"trustedSourceMatched"`
	ActionAllowed             bool                 `json:"actionAllowed"`
	ReasonCodes               []string             `json:"reasonCodes"`
}

// EvaluateManagementGuard gives exact management and recovery facts precedence
// over generic enforcement. Unknown or stale facts fail closed. Exemptions are
// source- and endpoint-specific; they never exempt unrelated VPN traffic.
func EvaluateManagementGuard(input ManagementGuardInput) ManagementGuardResult {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := ManagementGuardResult{State: ManagementGuardNotApplicable, ActionAllowed: true, ProtectedEndpointIDs: []string{}, FreshRecoveryPathIDs: []string{}, UnaffectedRecoveryPathIDs: []string{}, ReasonCodes: []string{}}
	if !input.MayRestrictTraffic {
		result.ReasonCodes = []string{"non_restrictive_action"}
		return result
	}
	if !validGuardSubject(input.Subject) {
		result.State = ManagementGuardUnknown
		result.ActionAllowed = false
		result.ReasonCodes = []string{"management_subject_unknown"}
		return result
	}
	for _, source := range input.TrustedSources {
		if subjectOverlapsPrefix(input.Subject, source) {
			result.State = ManagementGuardProtected
			result.TrustedSourceMatched = true
			result.ActionAllowed = false
			result.ReasonCodes = []string{"trusted_source_precedence"}
			return result
		}
	}

	matched := make([]hostresources.ManagementEndpointV1, 0)
	for _, endpoint := range input.Management {
		if managementEndpointMatches(endpoint, input.Scope.TargetResourceID, input.EndpointKey) {
			matched = append(matched, endpoint)
		}
	}
	if len(matched) == 0 {
		result.ReasonCodes = []string{"not_a_management_endpoint"}
		return result
	}
	result.State = ManagementGuardAllowed
	for _, endpoint := range matched {
		result.ProtectedEndpointIDs = append(result.ProtectedEndpointIDs, endpoint.ID)
		if !hostresources.ManagementEndpointCurrent(endpoint) {
			result.State = ManagementGuardUnknown
			result.ActionAllowed = false
			result.ReasonCodes = append(result.ReasonCodes, "management_endpoint_unknown")
		}
	}
	if !result.ActionAllowed {
		result.normalize()
		return result
	}

	unknownRecovery := false
	protectedRecovery := false
	for _, endpoint := range matched {
		freshForEndpoint := 0
		unaffectedForEndpoint := 0
		for _, path := range input.RecoveryPaths {
			if path.EndpointID != endpoint.ID || !strings.EqualFold(path.Kind, string(endpoint.ServiceKind)) || path.ConfigurationRevision != endpoint.ConfigurationRevision || !hostresources.RecoveryPathFresh(path, now) {
				continue
			}
			if prefix := strings.TrimSpace(path.SourcePrefix); prefix != "" {
				parsed, err := netip.ParsePrefix(prefix)
				if err != nil || ((endpoint.Family == hostresources.AddressFamilyIPv4) != parsed.Addr().Is4()) {
					continue
				}
			}
			freshForEndpoint++
			result.FreshRecoveryPathIDs = append(result.FreshRecoveryPathIDs, path.ID)
			if recoveryUnaffected(path, input.Subject) {
				unaffectedForEndpoint++
				result.UnaffectedRecoveryPathIDs = append(result.UnaffectedRecoveryPathIDs, path.ID)
			}
		}
		if freshForEndpoint == 0 {
			unknownRecovery = true
			result.ReasonCodes = append(result.ReasonCodes, "fresh_recovery_path_missing")
		} else if unaffectedForEndpoint == 0 {
			protectedRecovery = true
			result.ReasonCodes = append(result.ReasonCodes, "last_recovery_path_protected")
		}
	}
	if unknownRecovery {
		result.State = ManagementGuardUnknown
		result.ActionAllowed = false
	} else if protectedRecovery {
		result.State = ManagementGuardProtected
		result.ActionAllowed = false
	} else {
		result.ReasonCodes = append(result.ReasonCodes, "independent_recovery_path_preserved")
	}
	result.normalize()
	return result
}

func managementEndpointMatches(value hostresources.ManagementEndpointV1, resourceID string, key hostresources.PublicEndpointKey) bool {
	keyKnown := key.Network != hostresources.NetworkUnknown && key.AddressFamily != hostresources.AddressFamilyUnknown && key.Port != 0
	if keyKnown {
		if resourceID != "" && value.ResourceID != "" && value.ResourceID != resourceID {
			return false
		}
		return value.Network == key.Network && value.Family == key.AddressFamily && value.Port == key.Port && hostresources.NormalizeListen(value.Bind).Value == hostresources.NormalizeListen(key.BindAddress).Value
	}
	return resourceID != "" && value.ResourceID == resourceID
}

func recoveryUnaffected(path hostresources.RecoveryPathV1, subject domain.SignalSubjectV2) bool {
	method := strings.ToLower(strings.TrimSpace(path.VerificationMethod))
	if method == "provider_console" {
		return true
	}
	prefix := strings.TrimSpace(path.SourcePrefix)
	if prefix == "" {
		return false
	}
	return !subjectOverlapsPrefix(subject, prefix)
}

func subjectOverlapsPrefix(subject domain.SignalSubjectV2, prefixValue string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(prefixValue))
	if err != nil {
		return false
	}
	prefix = prefix.Masked()
	switch subject.Type {
	case "ip":
		address, parseErr := netip.ParseAddr(subject.Value)
		return parseErr == nil && prefix.Contains(address)
	case "prefix":
		other, parseErr := netip.ParsePrefix(subject.Value)
		if parseErr != nil {
			return false
		}
		other = other.Masked()
		return prefix.Contains(other.Addr()) || other.Contains(prefix.Addr())
	default:
		return false
	}
}

func validGuardSubject(subject domain.SignalSubjectV2) bool {
	switch subject.Type {
	case "ip":
		address, err := netip.ParseAddr(subject.Value)
		return err == nil && address.Unmap().String() == subject.Value
	case "prefix":
		prefix, err := netip.ParsePrefix(subject.Value)
		return err == nil && prefix.Masked().String() == subject.Value
	default:
		return false
	}
}

func (result *ManagementGuardResult) normalize() {
	result.ProtectedEndpointIDs = uniqueSorted(result.ProtectedEndpointIDs)
	result.FreshRecoveryPathIDs = uniqueSorted(result.FreshRecoveryPathIDs)
	result.UnaffectedRecoveryPathIDs = uniqueSorted(result.UnaffectedRecoveryPathIDs)
	result.ReasonCodes = uniqueSorted(result.ReasonCodes)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
