package fronting

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	NginxStrategyCapabilitySchemaV2 = "solovey-ui/nginx-strategy-capability/v2"
	FrontingSocketClaimSchemaV1     = "solovey-ui/fronting-socket-claim/v1"
	SelectorSetSchemaV1             = "solovey-ui/fronting-selector-set/v1"
	MaxFrontingRoutesV1             = 32
	MaxSelectorTuplesV1             = 128
	MaxALPNTokensPerSNIV1           = 16
	MaxFixedTargetsV1               = 64
	MaxFutureCandidateBytesV1       = 512 << 10
)

type FrontingStrategy string

const (
	StrategyL4OneToOne      FrontingStrategy = "L4_ONE_TO_ONE_FRONTING"
	StrategySNIPreread      FrontingStrategy = "SNI_PREREAD_FRONTING"
	StrategyHTTPTerminating FrontingStrategy = "HTTP_TERMINATING_FRONTING"
	StrategyUDPQUIC         FrontingStrategy = "UDP_QUIC"
)

type StrategySupportV2 string

const (
	StrategySupportedV2   StrategySupportV2 = "SUPPORTED"
	StrategyUnsupportedV2 StrategySupportV2 = "UNSUPPORTED"
	StrategyUnknownV2     StrategySupportV2 = "UNKNOWN"
)

type NginxStrategyCapabilityV2 struct {
	Schema                       string                  `json:"schema"`
	RuntimeIdentityRevision      string                  `json:"runtimeIdentityRevision"`
	Strategy                     FrontingStrategy        `json:"strategy"`
	CapabilityRevision           string                  `json:"capabilityRevision"`
	Stream                       NginxModuleCapabilityV2 `json:"stream"`
	SSLPreread                   NginxModuleCapabilityV2 `json:"sslPreread"`
	Validation                   NginxMethodCapabilityV2 `json:"validation"`
	Reload                       NginxMethodCapabilityV2 `json:"reload"`
	ActiveVerification           NginxMethodCapabilityV2 `json:"activeVerification"`
	ProcessVerification          NginxMethodCapabilityV2 `json:"processVerification"`
	ListenerVerification         NginxMethodCapabilityV2 `json:"listenerVerification"`
	SelectedProxyMode            hostresources.ProxyMode `json:"selectedProxyMode"`
	ProxyProtocolReceive         NginxMethodCapabilityV2 `json:"proxyProtocolReceive"`
	ProxyProtocolEmit            NginxMethodCapabilityV2 `json:"proxyProtocolEmit"`
	BackendProxyProtocolReceive  CapabilityTruthV2       `json:"backendProxyProtocolReceive"`
	ManagementExclusionsRevision string                  `json:"managementExclusionsRevision"`
	Support                      StrategySupportV2       `json:"support"`
	Actionable                   bool                    `json:"actionable"`
	InspectionOnly               bool                    `json:"inspectionOnly"`
	Blocks                       []string                `json:"blocks"`
	Warnings                     []string                `json:"warnings"`
	ReasonCodes                  []string                `json:"reasonCodes"`
	ObservedAt                   int64                   `json:"observedAt"`
	ExpiresAt                    int64                   `json:"expiresAt"`
}

func ResolveNginxStrategyCapabilityV2(identity NginxRuntimeIdentityV2, strategy FrontingStrategy, proxyMode hostresources.ProxyMode, backendProxyReceive CapabilityTruthV2, now time.Time) NginxStrategyCapabilityV2 {
	value := NginxStrategyCapabilityV2{Schema: NginxStrategyCapabilitySchemaV2,
		RuntimeIdentityRevision: identity.CanonicalRuntimeIdentityRevision, Strategy: strategy,
		Stream: identity.Stream, SSLPreread: identity.SSLPreread, Validation: identity.ValidationMethod,
		Reload: identity.ReloadMethod, ActiveVerification: identity.ActiveVerification,
		ProcessVerification: identity.ProcessVerification, ListenerVerification: identity.ListenerVerification,
		SelectedProxyMode: proxyMode, ProxyProtocolReceive: identity.ProxyProtocolReceive, ProxyProtocolEmit: identity.ProxyProtocolEmit,
		BackendProxyProtocolReceive:  backendProxyReceive,
		ManagementExclusionsRevision: identity.ManagementExclusionsRevision, Support: StrategyUnknownV2,
		Blocks: []string{}, Warnings: []string{}, ReasonCodes: []string{}, ObservedAt: identity.ObservedAt, ExpiresAt: identity.ExpiresAt}
	if err := identity.Validate(now); err != nil {
		value.Blocks = append(value.Blocks, "runtime identity is invalid or stale")
		value.ReasonCodes = append(value.ReasonCodes, "runtime_identity_invalid")
		return finalizeStrategyCapabilityV2(value)
	}
	switch strategy {
	case StrategyHTTPTerminating:
		value.Support = StrategyUnsupportedV2
		value.Blocks = append(value.Blocks, "HTTP terminating fronting is not shipped")
		value.ReasonCodes = append(value.ReasonCodes, "http_terminating_not_shipped")
		return finalizeStrategyCapabilityV2(value)
	case StrategyUDPQUIC:
		value.Support = StrategyUnsupportedV2
		value.Blocks = append(value.Blocks, "UDP and QUIC are outside the fronting contract")
		value.ReasonCodes = append(value.ReasonCodes, "udp_quic_out_of_scope")
		return finalizeStrategyCapabilityV2(value)
	case StrategyL4OneToOne, StrategySNIPreread:
	default:
		value.Blocks = append(value.Blocks, "strategy is unknown")
		value.ReasonCodes = append(value.ReasonCodes, "strategy_unknown")
		return finalizeStrategyCapabilityV2(value)
	}
	if proxyMode != hostresources.ProxyModeOff && proxyMode != hostresources.ProxyModeOn {
		value.Blocks = append(value.Blocks, "PROXY mode is unknown")
		value.ReasonCodes = append(value.ReasonCodes, "proxy_mode_unknown")
	}
	knownUnsupported := false
	if identity.State == NginxExternalManaged || identity.InstallationClass == NginxInstallationExternal || identity.InstallationClass == NginxInstallationDevelopment {
		value.InspectionOnly = true
		knownUnsupported = true
		value.Blocks = append(value.Blocks, "runtime is inspection-only")
		value.ReasonCodes = append(value.ReasonCodes, "runtime_inspection_only")
	}
	if identity.State != NginxManagedEngineReady {
		value.Blocks = append(value.Blocks, "managed engine identity is not ready")
		value.ReasonCodes = append(value.ReasonCodes, "managed_engine_not_ready")
	}
	if identity.State == NginxNotInstalled {
		knownUnsupported = true
	}
	if identity.Stream.Effective != CapabilitySupportedV2 {
		value.Blocks = append(value.Blocks, "stream capability is unavailable")
		value.ReasonCodes = append(value.ReasonCodes, "stream_unavailable")
		knownUnsupported = knownUnsupported || identity.Stream.Effective == CapabilityUnsupportedV2
	}
	if strategy == StrategySNIPreread && identity.SSLPreread.Effective != CapabilitySupportedV2 {
		value.Blocks = append(value.Blocks, "ssl_preread capability is unavailable")
		value.ReasonCodes = append(value.ReasonCodes, "ssl_preread_unavailable")
		knownUnsupported = knownUnsupported || identity.SSLPreread.Effective == CapabilityUnsupportedV2
	}
	for name, capability := range map[string]NginxMethodCapabilityV2{
		"validation": identity.ValidationMethod, "reload": identity.ReloadMethod,
		"active_verification": identity.ActiveVerification, "process_verification": identity.ProcessVerification,
		"listener_verification": identity.ListenerVerification,
	} {
		if capability.Availability != CapabilitySupportedV2 {
			value.Blocks = append(value.Blocks, strings.ReplaceAll(name, "_", " ")+" is unavailable")
			value.ReasonCodes = append(value.ReasonCodes, name+"_unavailable")
			knownUnsupported = knownUnsupported || capability.Availability == CapabilityUnsupportedV2
		}
	}
	if proxyMode == hostresources.ProxyModeOn {
		if identity.ProxyProtocolEmit.Availability != CapabilitySupportedV2 {
			value.Blocks = append(value.Blocks, "NGINX PROXY emit capability is unproven")
			value.ReasonCodes = append(value.ReasonCodes, "proxy_emit_unproven")
			knownUnsupported = knownUnsupported || identity.ProxyProtocolEmit.Availability == CapabilityUnsupportedV2
		}
		if backendProxyReceive != CapabilitySupportedV2 {
			value.Blocks = append(value.Blocks, "backend PROXY receive capability is unproven")
			value.ReasonCodes = append(value.ReasonCodes, "proxy_receive_unproven")
			knownUnsupported = knownUnsupported || backendProxyReceive == CapabilityUnsupportedV2
		}
	}
	if len(value.Blocks) == 0 {
		value.Support, value.Actionable = StrategySupportedV2, true
	} else if knownUnsupported {
		value.Support = StrategyUnsupportedV2
	}
	return finalizeStrategyCapabilityV2(value)
}

func finalizeStrategyCapabilityV2(value NginxStrategyCapabilityV2) NginxStrategyCapabilityV2 {
	value.Blocks = canonicalMessagesV2(value.Blocks)
	value.Warnings = canonicalMessagesV2(value.Warnings)
	value.ReasonCodes = canonicalRuntimeReasonsV2(value.ReasonCodes)
	value.CapabilityRevision = v2Revision(strategyCapabilityRevisionInput(value))
	return value
}

func strategyCapabilityRevisionInput(value NginxStrategyCapabilityV2) NginxStrategyCapabilityV2 {
	value.CapabilityRevision = ""
	value.ObservedAt, value.ExpiresAt = 0, 0
	return value
}

type FrontingSocketClaimV1 struct {
	Schema                               string                      `json:"schema"`
	ResourceID                           string                      `json:"resourceId"`
	EndpointID                           string                      `json:"endpointId"`
	AddressFamily                        hostresources.AddressFamily `json:"addressFamily"`
	CanonicalBind                        string                      `json:"canonicalBind"`
	Wildcard                             bool                        `json:"wildcard"`
	Protocol                             hostresources.Network       `json:"protocol"`
	PublicPort                           uint16                      `json:"publicPort"`
	CurrentConfigurationRevision         string                      `json:"currentConfigurationRevision"`
	TopologyOwnershipEligibilityRevision string                      `json:"topologyOwnershipEligibilityRevision"`
	ListenerSocketFactRevision           string                      `json:"listenerSocketFactRevision"`
	ManagementExclusionRevision          string                      `json:"managementExclusionRevision"`
	TopologyMutationEligible             bool                        `json:"topologyMutationEligible"`
	ClaimRevision                        string                      `json:"claimRevision"`
	ObservedAt                           int64                       `json:"observedAt"`
	ExpiresAt                            int64                       `json:"expiresAt"`
	ReasonCodes                          []string                    `json:"reasonCodes,omitempty"`
}

func FinalizeFrontingSocketClaimV1(value FrontingSocketClaimV1) (FrontingSocketClaimV1, error) {
	value.Schema = FrontingSocketClaimSchemaV1
	value.ReasonCodes = canonicalRuntimeReasonsV2(value.ReasonCodes)
	value.ClaimRevision = v2Revision(socketClaimRevisionInput(value))
	return value, value.Validate(time.Unix(value.ObservedAt, 0))
}

func (value FrontingSocketClaimV1) Validate(now time.Time) error {
	address, err := netip.ParseAddr(value.CanonicalBind)
	if err != nil || value.Schema != FrontingSocketClaimSchemaV1 || safeRuntimeTokenV2(value.ResourceID, 256) == "" ||
		safeRuntimeTokenV2(value.EndpointID, 128) == "" || value.Protocol != hostresources.NetworkTCP || value.PublicPort == 0 ||
		(value.AddressFamily != hostresources.AddressFamilyIPv4 && value.AddressFamily != hostresources.AddressFamilyIPv6) ||
		address.Is4In6() || value.CanonicalBind != address.String() || (value.AddressFamily == hostresources.AddressFamilyIPv4) != address.Is4() ||
		value.Wildcard != address.IsUnspecified() || !value.Wildcard && (!address.IsGlobalUnicast() || address.IsPrivate()) ||
		!frontingHexV2(value.CurrentConfigurationRevision) || !frontingHexV2(value.TopologyOwnershipEligibilityRevision) ||
		!frontingHexV2(value.ListenerSocketFactRevision) || !frontingHexV2(value.ManagementExclusionRevision) ||
		value.ObservedAt <= 0 || value.ExpiresAt <= value.ObservedAt || value.ExpiresAt-value.ObservedAt > int64(MaxRuntimeFreshnessV2/time.Second) ||
		value.ExpiresAt <= now.UTC().Unix() || !validRuntimeReasonsV2(value.ReasonCodes) ||
		!frontingHexV2(value.ClaimRevision) || value.ClaimRevision != v2Revision(socketClaimRevisionInput(value)) {
		return errors.New("fronting_socket_claim_v1_invalid")
	}
	return nil
}

func socketClaimRevisionInput(value FrontingSocketClaimV1) FrontingSocketClaimV1 {
	value.ClaimRevision = ""
	value.ObservedAt, value.ExpiresAt = 0, 0
	return value
}

type SelectorDefaultPolicy string

const (
	SelectorDefaultReject            SelectorDefaultPolicy = "REJECT"
	SelectorDefaultFixedSafe         SelectorDefaultPolicy = "FIXED_SAFE_DEFAULT"
	SelectorDefaultNonTLSFixedTarget SelectorDefaultPolicy = "NON_TLS_FIXED_TARGET"
)

type SelectorALPNSemanticsV1 string

const SelectorALPNExactTokenMembershipV1 SelectorALPNSemanticsV1 = "EXACT_TOKEN_MEMBERSHIP"

type SelectorDefaultV1 struct {
	Policy                  SelectorDefaultPolicy `json:"policy"`
	TargetReferenceRevision string                `json:"targetReferenceRevision,omitempty"`
}

type SelectorRouteInputV1 struct {
	SNI                     string   `json:"sni"`
	ALPN                    []string `json:"alpn,omitempty"`
	TargetReferenceRevision string   `json:"targetReferenceRevision"`
}

type SelectorTupleV1 struct {
	SNI                     string `json:"sni"`
	ALPN                    string `json:"alpn,omitempty"`
	SelectorID              string `json:"selectorId"`
	UpstreamID              string `json:"upstreamId"`
	TargetReferenceRevision string `json:"targetReferenceRevision"`
}

type SelectorSetV1 struct {
	Schema              string                  `json:"schema"`
	ALPNSemantics       SelectorALPNSemanticsV1 `json:"alpnSemantics"`
	Tuples              []SelectorTupleV1       `json:"tuples"`
	TargetRevisions     []string                `json:"targetRevisions"`
	Default             SelectorDefaultV1       `json:"default"`
	SelectorSetRevision string                  `json:"selectorSetRevision"`
}

func CanonicalizeSelectorSetV1(routes []SelectorRouteInputV1, policy SelectorDefaultV1) (SelectorSetV1, error) {
	return canonicalizeSelectorSetV1(routes, policy, generatedSelectorIDV1)
}

func canonicalizeSelectorSetV1(routes []SelectorRouteInputV1, policy SelectorDefaultV1, idFactory func(string, any) string) (SelectorSetV1, error) {
	// Route capacity is the number of exact SNI names, not the number of
	// normalized route records. A name may deliberately have one SNI-only
	// record and one or more ALPN records while the tuple bound remains the
	// independent hard ceiling.
	if len(routes) > MaxSelectorTuplesV1 {
		return SelectorSetV1{}, errors.New("selector_route_limit_exceeded")
	}
	if policy.Policy == "" {
		policy.Policy = SelectorDefaultReject
	}
	if policy.Policy == SelectorDefaultReject {
		if policy.TargetReferenceRevision != "" {
			return SelectorSetV1{}, errors.New("selector_default_target_forbidden")
		}
	} else if policy.Policy == SelectorDefaultFixedSafe || policy.Policy == SelectorDefaultNonTLSFixedTarget {
		if !frontingHexV2(policy.TargetReferenceRevision) {
			return SelectorSetV1{}, errors.New("selector_default_target_required")
		}
	} else {
		return SelectorSetV1{}, errors.New("selector_default_policy_invalid")
	}
	tuples := make([]SelectorTupleV1, 0, len(routes))
	targets := map[string]bool{}
	seenTuple, seenID := map[string]bool{}, map[string]bool{}
	upstreamTargets := map[string]string{}
	canonicalSource := map[string]string{}
	alpnTargetBySNI := map[string]string{}
	for _, route := range routes {
		sni, err := canonicalSNIV1(route.SNI)
		if err != nil || !frontingHexV2(route.TargetReferenceRevision) || len(route.ALPN) > MaxALPNTokensPerSNIV1 {
			return SelectorSetV1{}, errors.New("selector_route_invalid")
		}
		if previous, exists := canonicalSource[sni]; exists && previous != route.SNI {
			return SelectorSetV1{}, errors.New("selector_sni_canonical_collision")
		}
		canonicalSource[sni] = route.SNI
		if len(canonicalSource) > MaxFrontingRoutesV1 {
			return SelectorSetV1{}, errors.New("selector_route_limit_exceeded")
		}
		alpns := append([]string(nil), route.ALPN...)
		if len(alpns) == 0 {
			alpns = []string{""}
		}
		sort.Strings(alpns)
		for index, alpn := range alpns {
			if alpn != "" && !validALPNV1(alpn) || index > 0 && alpns[index-1] == alpn {
				return SelectorSetV1{}, errors.New("selector_alpn_invalid")
			}
			key := sni + "\x00" + alpn
			if seenTuple[key] {
				return SelectorSetV1{}, errors.New("selector_tuple_duplicate")
			}
			seenTuple[key] = true
			if alpn != "" {
				if previous, exists := alpnTargetBySNI[sni]; exists && previous != route.TargetReferenceRevision {
					return SelectorSetV1{}, errors.New("selector_alpn_target_ambiguous")
				}
				alpnTargetBySNI[sni] = route.TargetReferenceRevision
			}
			selectorID := idFactory("sel", struct{ SNI, ALPN string }{sni, alpn})
			upstreamID := idFactory("up", route.TargetReferenceRevision)
			if seenID[selectorID] {
				return SelectorSetV1{}, errors.New("selector_generated_id_collision")
			}
			seenID[selectorID] = true
			if previousTarget, exists := upstreamTargets[upstreamID]; exists && previousTarget != route.TargetReferenceRevision {
				return SelectorSetV1{}, errors.New("selector_generated_id_collision")
			}
			upstreamTargets[upstreamID] = route.TargetReferenceRevision
			tuples = append(tuples, SelectorTupleV1{SNI: sni, ALPN: alpn, SelectorID: selectorID, UpstreamID: upstreamID, TargetReferenceRevision: route.TargetReferenceRevision})
			targets[route.TargetReferenceRevision] = true
			if len(tuples) > MaxSelectorTuplesV1 {
				return SelectorSetV1{}, errors.New("selector_tuple_limit_exceeded")
			}
		}
	}
	if policy.TargetReferenceRevision != "" {
		targets[policy.TargetReferenceRevision] = true
		upstreamID := idFactory("up", policy.TargetReferenceRevision)
		if previousTarget, exists := upstreamTargets[upstreamID]; exists && previousTarget != policy.TargetReferenceRevision {
			return SelectorSetV1{}, errors.New("selector_generated_id_collision")
		}
		upstreamTargets[upstreamID] = policy.TargetReferenceRevision
	}
	if len(targets) > MaxFixedTargetsV1 {
		return SelectorSetV1{}, errors.New("selector_target_limit_exceeded")
	}
	sort.Slice(tuples, func(i, j int) bool {
		left, right := tuples[i], tuples[j]
		return left.SNI+"\x00"+left.ALPN+"\x00"+left.TargetReferenceRevision < right.SNI+"\x00"+right.ALPN+"\x00"+right.TargetReferenceRevision
	})
	targetRevisions := make([]string, 0, len(targets))
	for target := range targets {
		targetRevisions = append(targetRevisions, target)
	}
	sort.Strings(targetRevisions)
	value := SelectorSetV1{Schema: SelectorSetSchemaV1, ALPNSemantics: SelectorALPNExactTokenMembershipV1, Tuples: tuples, TargetRevisions: targetRevisions, Default: policy}
	value.SelectorSetRevision = v2Revision(selectorSetRevisionInput(value))
	return value, nil
}

func (value SelectorSetV1) Validate() error {
	if value.Schema != SelectorSetSchemaV1 || value.ALPNSemantics != SelectorALPNExactTokenMembershipV1 || len(value.Tuples) > MaxSelectorTuplesV1 || len(value.TargetRevisions) > MaxFixedTargetsV1 ||
		!frontingHexV2(value.SelectorSetRevision) || value.SelectorSetRevision != v2Revision(selectorSetRevisionInput(value)) {
		return errors.New("selector_set_v1_invalid")
	}
	if value.Default.Policy == SelectorDefaultReject {
		if value.Default.TargetReferenceRevision != "" {
			return errors.New("selector_default_target_forbidden")
		}
	} else if value.Default.Policy == SelectorDefaultFixedSafe || value.Default.Policy == SelectorDefaultNonTLSFixedTarget {
		if !frontingHexV2(value.Default.TargetReferenceRevision) {
			return errors.New("selector_default_target_required")
		}
	} else {
		return errors.New("selector_default_policy_invalid")
	}
	targets := map[string]bool{}
	for index, revision := range value.TargetRevisions {
		if !frontingHexV2(revision) || index > 0 && value.TargetRevisions[index-1] >= revision {
			return errors.New("selector_target_revision_invalid")
		}
		targets[revision] = true
	}
	if value.Default.TargetReferenceRevision != "" && !targets[value.Default.TargetReferenceRevision] {
		return errors.New("selector_default_target_missing")
	}
	seenTuple, seenSelector := map[string]bool{}, map[string]bool{}
	sniCounts := map[string]int{}
	seenSNI := map[string]bool{}
	alpnTargetBySNI := map[string]string{}
	previous := ""
	for _, tuple := range value.Tuples {
		sni, err := canonicalSNIV1(tuple.SNI)
		if err != nil || sni != tuple.SNI || tuple.ALPN != "" && !validALPNV1(tuple.ALPN) ||
			!frontingHexV2(tuple.TargetReferenceRevision) || !targets[tuple.TargetReferenceRevision] ||
			tuple.SelectorID != generatedSelectorIDV1("sel", struct{ SNI, ALPN string }{tuple.SNI, tuple.ALPN}) ||
			tuple.UpstreamID != generatedSelectorIDV1("up", tuple.TargetReferenceRevision) {
			return errors.New("selector_tuple_invalid")
		}
		key := tuple.SNI + "\x00" + tuple.ALPN
		orderKey := key + "\x00" + tuple.TargetReferenceRevision
		if seenTuple[key] || seenSelector[tuple.SelectorID] || previous != "" && previous >= orderKey {
			return errors.New("selector_tuple_duplicate")
		}
		seenTuple[key], seenSelector[tuple.SelectorID], previous = true, true, orderKey
		seenSNI[tuple.SNI] = true
		if tuple.ALPN != "" {
			sniCounts[tuple.SNI]++
			if previousTarget, exists := alpnTargetBySNI[tuple.SNI]; exists && previousTarget != tuple.TargetReferenceRevision {
				return errors.New("selector_alpn_target_ambiguous")
			}
			alpnTargetBySNI[tuple.SNI] = tuple.TargetReferenceRevision
		}
		if sniCounts[tuple.SNI] > MaxALPNTokensPerSNIV1 || len(seenSNI) > MaxFrontingRoutesV1 {
			return errors.New("selector_tuple_bounds_invalid")
		}
	}
	return nil
}

func selectorSetRevisionInput(value SelectorSetV1) SelectorSetV1 {
	value.SelectorSetRevision = ""
	return value
}

func canonicalSNIV1(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 253 || strings.HasSuffix(value, ".") {
		return "", errors.New("selector_sni_invalid")
	}
	for _, r := range value {
		if r > 127 {
			return "", errors.New("selector_sni_non_ascii")
		}
	}
	value = strings.ToLower(value)
	if strings.Contains(value, "*") {
		return "", errors.New("selector_sni_wildcard_forbidden")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("selector_sni_label_invalid")
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return "", errors.New("selector_sni_label_invalid")
			}
		}
	}
	return value, nil
}

func validALPNV1(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e || strings.ContainsRune("\\\"'{},$;", r) {
			return false
		}
	}
	lower := strings.ToLower(value)
	return !strings.Contains(lower, "include") && !strings.Contains(lower, "proxy_pass") && !strings.Contains(lower, "map")
}

func generatedSelectorIDV1(prefix string, value any) string {
	revision := v2Revision(value)
	return prefix + "_" + revision[:24]
}

func canonicalMessagesV2(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 160 {
			continue
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func backendProxyTruthV2(value hostresources.CapabilityValue) CapabilityTruthV2 {
	switch value {
	case hostresources.CapabilityYes:
		return CapabilitySupportedV2
	case hostresources.CapabilityNo:
		return CapabilityUnsupportedV2
	default:
		return CapabilityUnknownV2
	}
}

func exactBackendKeyV2(provider, resource, endpoint string) string {
	return fmt.Sprintf("%s\x00%s\x00%s", provider, resource, endpoint)
}
