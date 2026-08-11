package fronting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	sniPrereadConnectTimeoutV2  = "5s"
	sniPrereadSessionTimeoutV2  = "1h"
	sniPrereadTimeoutV2         = "5s"
	sniPrereadBufferV2          = "16k"
	fixedSafeDefaultMaxLabelsV2 = 3
)

type SNITargetKindV2 string

const (
	SNITargetOrdinaryV2 SNITargetKindV2 = "ORDINARY_BACKEND"
	SNITargetFallbackV2 SNITargetKindV2 = "FALLBACK_TARGET"
)

type SNITargetBindingV2 struct {
	Kind              SNITargetKindV2             `json:"kind"`
	ReferenceRevision string                      `json:"referenceRevision"`
	EndpointRevision  string                      `json:"endpointRevision"`
	ProviderRevision  string                      `json:"providerRevision"`
	HealthRevision    string                      `json:"healthRevision"`
	CapacityRevision  string                      `json:"capacityRevision,omitempty"`
	AuthorityRevision string                      `json:"authorityRevision"`
	UpstreamID        string                      `json:"upstreamId"`
	Address           string                      `json:"address"`
	Port              uint16                      `json:"port"`
	AddressFamily     hostresources.AddressFamily `json:"addressFamily"`
	SelectedProxyMode hostresources.ProxyMode     `json:"selectedProxyMode"`
}

type sniTargetMetadataBindingV2 struct {
	Kind              SNITargetKindV2
	EndpointRevision  string
	ProviderRevision  string
	HealthRevision    string
	CapacityRevision  string
	SelectedProxyMode hostresources.ProxyMode
}

func sniTargetMetadataV2(target SNITargetBindingV2) sniTargetMetadataBindingV2 {
	return sniTargetMetadataBindingV2{Kind: target.Kind, EndpointRevision: target.EndpointRevision, ProviderRevision: target.ProviderRevision,
		HealthRevision: target.HealthRevision, CapacityRevision: target.CapacityRevision, SelectedProxyMode: target.SelectedProxyMode}
}

func expectedSNITargetMetadataV2(plan FrontingStrategyPlanV2) map[string]sniTargetMetadataBindingV2 {
	result := make(map[string]sniTargetMetadataBindingV2, len(plan.Targets.ReferenceRevisions))
	for _, reference := range plan.Targets.BackendReferences {
		result[reference.CanonicalReferenceRevision] = sniTargetMetadataBindingV2{Kind: SNITargetOrdinaryV2,
			EndpointRevision: reference.EndpointRevision, ProviderRevision: reference.ProviderRevision, HealthRevision: reference.HealthRevision,
			CapacityRevision: reference.CapacityRevision, SelectedProxyMode: reference.SelectedProxyMode}
	}
	for _, reference := range plan.Targets.FallbackReferences {
		result[v2Revision(reference)] = sniTargetMetadataBindingV2{Kind: SNITargetFallbackV2,
			EndpointRevision: reference.EndpointRevision, ProviderRevision: reference.ProviderRevision, HealthRevision: reference.ProviderHealthRevision,
			CapacityRevision: reference.CapacityRevision, SelectedProxyMode: plan.Targets.SelectedProxyMode}
	}
	return result
}

type SNIPrereadCandidateV2 struct {
	Revision              string
	SHA256                string
	Bytes                 []byte
	CanonicalInput        []byte
	Listener              protectionListenerV2
	SelectorSetRevision   string
	MapRevision           string
	UpstreamIDSetRevision string
	RejectID              string
	Targets               []SNITargetBindingV2
}

type sniMapRuleV2 struct {
	Pattern    string `json:"pattern"`
	UpstreamID string `json:"upstreamId"`
	Class      string `json:"class"`
	SelectorID string `json:"selectorId,omitempty"`
}

type FiniteSelectorResolutionV2 struct {
	Class                   string
	TargetReferenceRevision string
	UpstreamID              string
	Rejected                bool
}

// ResolveFiniteSelectorV2 is the pure selector-contract oracle used by tests
// and strategy health. Production SNI preread planning rejects every ALPN
// tuple because NGINX's comma projection cannot prove token boundaries.
func ResolveFiniteSelectorV2(set SelectorSetV1, serverName, advertisedALPN string) FiniteSelectorResolutionV2 {
	if set.Validate() != nil {
		return FiniteSelectorResolutionV2{Class: "INVALID", Rejected: true}
	}
	canonical, err := canonicalSNIV1(serverName)
	if err != nil || canonical != strings.ToLower(serverName) {
		return FiniteSelectorResolutionV2{Class: "REJECT", Rejected: true}
	}
	tokens := strings.Split(advertisedALPN, ",")
	tokenSet := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		if token != "" {
			tokenSet[token] = true
		}
	}
	matchedTarget := ""
	matchedUpstream := ""
	for _, tuple := range set.Tuples {
		if tuple.SNI != canonical || tuple.ALPN == "" || !tokenSet[tuple.ALPN] {
			continue
		}
		if matchedTarget != "" && matchedTarget != tuple.TargetReferenceRevision {
			return FiniteSelectorResolutionV2{Class: "AMBIGUOUS_REJECT", Rejected: true}
		}
		matchedTarget, matchedUpstream = tuple.TargetReferenceRevision, tuple.UpstreamID
	}
	if matchedTarget != "" {
		return FiniteSelectorResolutionV2{Class: "SNI_ALPN", TargetReferenceRevision: matchedTarget, UpstreamID: matchedUpstream}
	}
	for _, tuple := range set.Tuples {
		if tuple.SNI == canonical && tuple.ALPN == "" {
			return FiniteSelectorResolutionV2{Class: "SNI_ONLY", TargetReferenceRevision: tuple.TargetReferenceRevision, UpstreamID: tuple.UpstreamID}
		}
	}
	if set.Default.Policy == SelectorDefaultFixedSafe && fixedSafeDefaultSNIEligibleV2(serverName) {
		return FiniteSelectorResolutionV2{Class: "FIXED_SAFE_DEFAULT", TargetReferenceRevision: set.Default.TargetReferenceRevision,
			UpstreamID: generatedSelectorIDV1("up", set.Default.TargetReferenceRevision)}
	}
	return FiniteSelectorResolutionV2{Class: "REJECT", Rejected: true}
}

type sniCandidateBindingV2 struct {
	Schema                  string                  `json:"schema"`
	PlanDigest              string                  `json:"planDigest"`
	Strategy                FrontingStrategy        `json:"strategy"`
	RuntimeIdentityRevision string                  `json:"runtimeIdentityRevision"`
	CapabilityRevision      string                  `json:"capabilityRevision"`
	SocketClaimRevision     string                  `json:"socketClaimRevision"`
	ConfigurationRevision   string                  `json:"configurationRevision"`
	TopologyRevision        string                  `json:"topologyRevision"`
	ListenerFactRevision    string                  `json:"listenerFactRevision"`
	ManagementRevision      string                  `json:"managementRevision"`
	SelectorSetRevision     string                  `json:"selectorSetRevision"`
	ALPNSemantics           SelectorALPNSemanticsV1 `json:"alpnSemantics"`
	DefaultPolicy           SelectorDefaultPolicy   `json:"defaultPolicy"`
	MapRevision             string                  `json:"mapRevision"`
	UpstreamIDSetRevision   string                  `json:"upstreamIdSetRevision"`
	RejectID                string                  `json:"rejectId"`
	Targets                 []SNITargetBindingV2    `json:"targets"`
	ProxyMode               hostresources.ProxyMode `json:"proxyMode"`
	ConnectTimeout          string                  `json:"connectTimeout"`
	SessionTimeout          string                  `json:"sessionTimeout"`
	PrereadTimeout          string                  `json:"prereadTimeout"`
	PrereadBuffer           string                  `json:"prereadBuffer"`
}

// RenderSNIPrereadCandidateV2 accepts only the exact current plan input and a
// provider-authority revision for every selected target. ClientHello facts can
// select only the finite generated upstream IDs emitted by this function.
func RenderSNIPrereadCandidateV2(plan FrontingStrategyPlanV2, input FrontingPlanInputV2, authorityRevisions map[string]string, now time.Time) (SNIPrereadCandidateV2, error) {
	if err := validateSNIPlanShapeV2(plan, now); err != nil {
		return SNIPrereadCandidateV2{}, err
	}
	if input.Now.IsZero() {
		input.Now = time.Unix(plan.CreatedAt, 0).UTC()
	}
	regenerated, err := PlanFrontingStrategyV2(input)
	if err != nil || regenerated.CanonicalPlanDigest != plan.CanonicalPlanDigest {
		return SNIPrereadCandidateV2{}, errors.New("plan_stale")
	}
	targets, err := resolveSNITargetBindingsV2(plan, input, authorityRevisions, now)
	if err != nil {
		return SNIPrereadCandidateV2{}, err
	}
	rules, rejectID, err := sniMapRulesV2(plan)
	if err != nil {
		return SNIPrereadCandidateV2{}, err
	}
	mapRevision := v2Revision(struct {
		Selector string
		Default  SelectorDefaultV1
		Rules    []sniMapRuleV2
		Reject   string
	}{plan.Selectors.SelectorSetRevision, plan.Selectors.Default, rules, rejectID})
	upstreamIDs := []string{rejectID}
	for _, target := range targets {
		upstreamIDs = append(upstreamIDs, target.UpstreamID)
	}
	sort.Strings(upstreamIDs)
	for index := 1; index < len(upstreamIDs); index++ {
		if upstreamIDs[index-1] == upstreamIDs[index] {
			return SNIPrereadCandidateV2{}, errors.New("selector_generated_id_collision")
		}
	}
	upstreamRevision := v2Revision(upstreamIDs)
	binding := sniCandidateBindingV2{
		Schema: "solovey-ui/sni-preread-candidate/v2", PlanDigest: plan.CanonicalPlanDigest, Strategy: StrategySNIPreread,
		RuntimeIdentityRevision: plan.Runtime.IdentityRevision, CapabilityRevision: plan.StrategyCapabilityRevision,
		SocketClaimRevision: plan.PublicSocket.ClaimRevision, ConfigurationRevision: plan.PublicSocket.CurrentConfigurationRevision,
		TopologyRevision: plan.PublicSocket.TopologyOwnershipEligibilityRevision, ListenerFactRevision: plan.PublicSocket.ListenerSocketFactRevision,
		ManagementRevision: plan.PublicSocket.ManagementExclusionRevision, SelectorSetRevision: plan.Selectors.SelectorSetRevision,
		ALPNSemantics: plan.Selectors.ALPNSemantics, DefaultPolicy: plan.Selectors.Default.Policy, MapRevision: mapRevision,
		UpstreamIDSetRevision: upstreamRevision, RejectID: rejectID, Targets: targets, ProxyMode: plan.Targets.SelectedProxyMode,
		ConnectTimeout: sniPrereadConnectTimeoutV2, SessionTimeout: sniPrereadSessionTimeoutV2,
		PrereadTimeout: sniPrereadTimeoutV2, PrereadBuffer: sniPrereadBufferV2,
	}
	canonical, err := json.Marshal(binding)
	if err != nil {
		return SNIPrereadCandidateV2{}, errors.New("candidate_invalid")
	}
	revision := v2Revision(binding)
	listen := net.JoinHostPort(plan.PublicSocket.CanonicalBind, fmt.Sprint(plan.PublicSocket.PublicPort))
	var output strings.Builder
	output.Grow(2048 + len(rules)*160 + len(targets)*160)
	output.WriteString("# generated by Solovey Server Protection; do not edit\n")
	output.WriteString("# solovey-strategy:SNI_PREREAD_FRONTING\n# solovey-plan:")
	output.WriteString(plan.CanonicalPlanDigest)
	output.WriteString("\n# solovey-revision:")
	output.WriteString(revision)
	output.WriteString("\n# solovey-selector-set:")
	output.WriteString(plan.Selectors.SelectorSetRevision)
	output.WriteString("\n# solovey-map:")
	output.WriteString(mapRevision)
	output.WriteString("\nmap $ssl_preread_server_name $solovey_selected_upstream {\n  default ")
	output.WriteString(rejectID)
	output.WriteString(";\n")
	for _, rule := range rules {
		output.WriteString("  ~")
		output.WriteString(rule.Pattern)
		output.WriteByte(' ')
		output.WriteString(rule.UpstreamID)
		output.WriteString(";\n")
	}
	output.WriteString("}\n\nupstream ")
	output.WriteString(rejectID)
	output.WriteString(" {\n  server 127.0.0.1:1 down;\n}\n")
	for _, target := range targets {
		output.WriteString("\nupstream ")
		output.WriteString(target.UpstreamID)
		output.WriteString(" {\n  server ")
		output.WriteString(net.JoinHostPort(target.Address, fmt.Sprint(target.Port)))
		output.WriteString(";\n}\n")
	}
	output.WriteString("\nserver {\n  listen ")
	output.WriteString(listen)
	output.WriteString(";\n  preread_buffer_size ")
	output.WriteString(sniPrereadBufferV2)
	output.WriteString(";\n  preread_timeout ")
	output.WriteString(sniPrereadTimeoutV2)
	output.WriteString(";\n  proxy_connect_timeout ")
	output.WriteString(sniPrereadConnectTimeoutV2)
	output.WriteString(";\n  proxy_timeout ")
	output.WriteString(sniPrereadSessionTimeoutV2)
	output.WriteString(";\n  ssl_preread on;\n")
	if plan.Targets.SelectedProxyMode == hostresources.ProxyModeOn {
		output.WriteString("  proxy_protocol on;\n")
	}
	output.WriteString("  proxy_pass $solovey_selected_upstream;\n}\n")
	data := []byte(output.String())
	candidate := SNIPrereadCandidateV2{Revision: revision, SHA256: fixedL4DigestV2(data), Bytes: data, CanonicalInput: canonical,
		Listener:            protectionListenerV2{Address: plan.PublicSocket.CanonicalBind, Port: int(plan.PublicSocket.PublicPort)},
		SelectorSetRevision: plan.Selectors.SelectorSetRevision, MapRevision: mapRevision, UpstreamIDSetRevision: upstreamRevision,
		RejectID: rejectID, Targets: targets}
	if err := ValidateSNIPrereadCandidateV2(candidate, plan); err != nil {
		return SNIPrereadCandidateV2{}, err
	}
	return candidate, nil
}

func resolveSNITargetBindingsV2(plan FrontingStrategyPlanV2, input FrontingPlanInputV2, authorities map[string]string, now time.Time) ([]SNITargetBindingV2, error) {
	if len(authorities) != len(plan.Selectors.TargetRevisions) {
		return nil, errors.New("target_authority_stale")
	}
	ordinary := make(map[string]SNITargetBindingV2, len(plan.Targets.BackendReferences))
	for _, reference := range plan.Targets.BackendReferences {
		var matched *hostresources.FrontingBackendFactV1
		for index := range input.Inventory {
			fact := &input.Inventory[index]
			if fact.ProviderID == reference.ProviderID && fact.ResourceID == reference.ResourceID && fact.EndpointID == reference.EndpointID {
				matched = fact
				break
			}
		}
		if matched == nil {
			return nil, errors.New("backend_reference_stale")
		}
		endpoint, err := hostresources.ResolveFrontingBackendEndpointV1(reference, *matched, now)
		if err != nil || matched.CanReachManagement != hostresources.CapabilityNo {
			return nil, errors.New("backend_reference_stale")
		}
		revision := reference.CanonicalReferenceRevision
		ordinary[revision] = SNITargetBindingV2{Kind: SNITargetOrdinaryV2, ReferenceRevision: revision,
			EndpointRevision: reference.EndpointRevision, ProviderRevision: reference.ProviderRevision, HealthRevision: reference.HealthRevision,
			CapacityRevision: reference.CapacityRevision, UpstreamID: generatedSelectorIDV1("up", revision), Address: endpoint.Address.String(),
			Port: endpoint.Port, AddressFamily: endpoint.AddressFamily, SelectedProxyMode: reference.SelectedProxyMode}
	}
	fallback := make(map[string]SNITargetBindingV2, len(plan.Targets.FallbackReferences))
	for _, reference := range plan.Targets.FallbackReferences {
		var matched *FallbackPlanningTargetV2
		for index := range input.FallbackTargets {
			item := &input.FallbackTargets[index]
			if item.Reference == reference {
				matched = item
				break
			}
		}
		if matched == nil || fallbacktargets.ResolveExactV2(reference, matched.Target, now) != nil {
			return nil, errors.New("fallback_reference_stale")
		}
		endpoint := matched.Target.Endpoint
		address, err := netip.ParseAddr(endpoint.Address)
		if err != nil || address.Is4In6() || endpoint.Network != hostresources.NetworkTCP || !endpoint.Local || !address.IsLoopback() || endpoint.Port == 0 ||
			(endpoint.AddressFamily == hostresources.AddressFamilyIPv4) != address.Is4() || endpoint.CanReachManagement != hostresources.CapabilityNo ||
			(plan.Targets.SelectedProxyMode == hostresources.ProxyModeOn && endpoint.ProxyProtocol != hostresources.CapabilityYes) {
			return nil, errors.New("fallback_reference_stale")
		}
		revision := v2Revision(reference)
		fallback[revision] = SNITargetBindingV2{Kind: SNITargetFallbackV2, ReferenceRevision: revision,
			EndpointRevision: reference.EndpointRevision, ProviderRevision: reference.ProviderRevision, HealthRevision: reference.ProviderHealthRevision,
			CapacityRevision: reference.CapacityRevision, UpstreamID: generatedSelectorIDV1("up", revision), Address: address.String(), Port: endpoint.Port,
			AddressFamily: endpoint.AddressFamily, SelectedProxyMode: plan.Targets.SelectedProxyMode}
	}
	result := make([]SNITargetBindingV2, 0, len(plan.Selectors.TargetRevisions))
	for _, revision := range plan.Selectors.TargetRevisions {
		authority := authorities[revision]
		if !frontingHexV2(authority) {
			return nil, errors.New("target_authority_stale")
		}
		binding, ok := ordinary[revision]
		if !ok {
			binding, ok = fallback[revision]
		}
		if !ok || binding.SelectedProxyMode != plan.Targets.SelectedProxyMode {
			return nil, errors.New("selector_target_missing")
		}
		binding.AuthorityRevision = authority
		result = append(result, binding)
	}
	return result, nil
}

func sniMapRulesV2(plan FrontingStrategyPlanV2) ([]sniMapRuleV2, string, error) {
	rejectID := "reject_" + v2Revision(struct {
		Plan     string
		Selector string
		Policy   SelectorDefaultPolicy
	}{plan.CanonicalPlanDigest, plan.Selectors.SelectorSetRevision, plan.Selectors.Default.Policy})[:24]
	if selectorSetHasALPNV2(plan.Selectors) {
		return nil, "", errors.New("alpn_exact_projection_unavailable")
	}
	rules := make([]sniMapRuleV2, 0, len(plan.Selectors.Tuples)+1)
	for _, tuple := range plan.Selectors.Tuples {
		rules = append(rules, sniMapRuleV2{Pattern: "^" + caseFoldSNIRegexV2(tuple.SNI) + "$", UpstreamID: tuple.UpstreamID, Class: "SNI_ONLY", SelectorID: tuple.SelectorID})
	}
	if plan.Selectors.Default.Policy == SelectorDefaultFixedSafe {
		rules = append(rules, sniMapRuleV2{Pattern: `^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?){0,2}$`,
			UpstreamID: generatedSelectorIDV1("up", plan.Selectors.Default.TargetReferenceRevision), Class: "FIXED_SAFE_DEFAULT"})
	}
	for _, rule := range rules {
		if !strings.HasPrefix(rule.Pattern, "^") || !strings.HasSuffix(rule.Pattern, "$") || !validGeneratedMapIDV2(rule.UpstreamID) || strings.Contains(rule.Pattern, "(?=") || strings.Contains(rule.Pattern, "(?<") {
			return nil, "", errors.New("candidate_invalid")
		}
	}
	return rules, rejectID, nil
}

func fixedSafeDefaultSNIEligibleV2(value string) bool {
	canonical, err := canonicalSNIV1(value)
	return err == nil && canonical == strings.ToLower(value) && len(strings.Split(canonical, ".")) <= fixedSafeDefaultMaxLabelsV2
}

func caseFoldSNIRegexV2(value string) string {
	var output strings.Builder
	output.Grow(len(value) * 4)
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
			output.WriteByte('[')
			output.WriteRune(character)
			output.WriteRune(character - ('a' - 'A'))
			output.WriteByte(']')
		case character == '.':
			output.WriteString(`\.`)
		default:
			output.WriteRune(character)
		}
	}
	return output.String()
}

func ValidateSNIPrereadCandidateV2(candidate SNIPrereadCandidateV2, plan FrontingStrategyPlanV2) error {
	if len(candidate.Bytes) == 0 || len(candidate.Bytes) > MaxFutureCandidateBytesV1 {
		if len(candidate.Bytes) > MaxFutureCandidateBytesV1 {
			return errors.New("candidate_too_large")
		}
		return errors.New("candidate_invalid")
	}
	var binding sniCandidateBindingV2
	if json.Unmarshal(candidate.CanonicalInput, &binding) != nil {
		return errors.New("candidate_identity_invalid")
	}
	canonicalBinding, err := json.Marshal(binding)
	if err != nil || !bytes.Equal(canonicalBinding, candidate.CanonicalInput) || candidate.Revision != v2Revision(binding) ||
		binding.Schema != "solovey-ui/sni-preread-candidate/v2" || binding.PlanDigest != plan.CanonicalPlanDigest || binding.Strategy != StrategySNIPreread ||
		binding.RuntimeIdentityRevision != plan.Runtime.IdentityRevision || binding.CapabilityRevision != plan.StrategyCapabilityRevision ||
		binding.SocketClaimRevision != plan.PublicSocket.ClaimRevision || binding.ConfigurationRevision != plan.PublicSocket.CurrentConfigurationRevision ||
		binding.TopologyRevision != plan.PublicSocket.TopologyOwnershipEligibilityRevision || binding.ListenerFactRevision != plan.PublicSocket.ListenerSocketFactRevision ||
		binding.ManagementRevision != plan.PublicSocket.ManagementExclusionRevision || binding.SelectorSetRevision != candidate.SelectorSetRevision ||
		binding.ALPNSemantics != plan.Selectors.ALPNSemantics || binding.DefaultPolicy != plan.Selectors.Default.Policy ||
		binding.MapRevision != candidate.MapRevision || binding.UpstreamIDSetRevision != candidate.UpstreamIDSetRevision || binding.RejectID != candidate.RejectID ||
		v2Revision(binding.Targets) != v2Revision(candidate.Targets) || binding.ProxyMode != plan.Targets.SelectedProxyMode ||
		binding.ConnectTimeout != sniPrereadConnectTimeoutV2 || binding.SessionTimeout != sniPrereadSessionTimeoutV2 ||
		binding.PrereadTimeout != sniPrereadTimeoutV2 || binding.PrereadBuffer != sniPrereadBufferV2 {
		return errors.New("candidate_identity_invalid")
	}
	if !frontingHexV2(candidate.Revision) || candidate.SHA256 != fixedL4DigestV2(candidate.Bytes) || !frontingHexV2(candidate.SelectorSetRevision) ||
		!frontingHexV2(candidate.MapRevision) || !frontingHexV2(candidate.UpstreamIDSetRevision) || !validGeneratedMapIDV2(candidate.RejectID) ||
		candidate.SelectorSetRevision != plan.Selectors.SelectorSetRevision || len(candidate.Targets) != len(plan.Selectors.TargetRevisions) {
		return errors.New("candidate_identity_invalid")
	}
	allowed := map[string]bool{candidate.RejectID: true}
	expectedTargets := expectedSNITargetMetadataV2(plan)
	for index, target := range candidate.Targets {
		address, addressErr := netip.ParseAddr(target.Address)
		expected, expectedOK := expectedTargets[target.ReferenceRevision]
		if index > 0 && candidate.Targets[index-1].ReferenceRevision >= target.ReferenceRevision || target.ReferenceRevision != plan.Selectors.TargetRevisions[index] ||
			!frontingHexV2(target.ReferenceRevision) || !frontingHexV2(target.EndpointRevision) || !frontingHexV2(target.HealthRevision) ||
			target.CapacityRevision != "" && !frontingHexV2(target.CapacityRevision) || !frontingHexV2(target.AuthorityRevision) ||
			target.UpstreamID != generatedSelectorIDV1("up", target.ReferenceRevision) || !validGeneratedMapIDV2(target.UpstreamID) ||
			addressErr != nil || address.Is4In6() || !address.IsLoopback() || target.Port == 0 ||
			(target.AddressFamily == hostresources.AddressFamilyIPv4) != address.Is4() || !expectedOK || expected != sniTargetMetadataV2(target) {
			return errors.New("candidate_target_binding_invalid")
		}
		allowed[target.UpstreamID] = true
	}
	rules, rejectID, err := sniMapRulesV2(plan)
	if err != nil || rejectID != candidate.RejectID {
		return errors.New("candidate_identity_invalid")
	}
	expectedMapLines := map[string]bool{"default " + rejectID + ";": true}
	for _, rule := range rules {
		expectedMapLines["~"+rule.Pattern+" "+rule.UpstreamID+";"] = true
	}
	expectedMapRevision := v2Revision(struct {
		Selector string
		Default  SelectorDefaultV1
		Rules    []sniMapRuleV2
		Reject   string
	}{plan.Selectors.SelectorSetRevision, plan.Selectors.Default, rules, rejectID})
	upstreamIDs := make([]string, 0, len(allowed))
	for id := range allowed {
		upstreamIDs = append(upstreamIDs, id)
	}
	sort.Strings(upstreamIDs)
	if candidate.MapRevision != expectedMapRevision || candidate.UpstreamIDSetRevision != v2Revision(upstreamIDs) {
		return errors.New("candidate_identity_invalid")
	}
	text := string(candidate.Bytes)
	lower := strings.ToLower(text)
	for _, forbidden := range []string{"resolver ", "ssl_certificate", "ssl_certificate_key", "proxy_ssl", "listen  udp", " unix:", "include ", "backup", "proxy_next_upstream", "\n  server_name ", "location ", "http {", "host", "proxy_pass $ssl_preread", "proxy_pass $1", "proxy_pass $2"} {
		if strings.Contains(lower, forbidden) {
			return errors.New("candidate_forbidden_grammar")
		}
	}
	if strings.Count(lower, "map ") != 1 || strings.Count(lower, "server {") != 1 || strings.Count(lower, "  listen ") != 1 ||
		strings.Count(lower, "proxy_pass ") != 1 || !strings.Contains(text, "ssl_preread on;") ||
		!strings.Contains(text, "proxy_pass $solovey_selected_upstream;") || strings.Contains(lower, "ssl on;") {
		return errors.New("candidate_shape_invalid")
	}
	if strings.Count(text, "\n  server ") != len(candidate.Targets)+1 ||
		!strings.Contains(text, "upstream "+candidate.RejectID+" {\n  server 127.0.0.1:1 down;\n}") {
		return errors.New("candidate_target_binding_invalid")
	}
	for _, target := range candidate.Targets {
		block := "upstream " + target.UpstreamID + " {\n  server " + net.JoinHostPort(target.Address, fmt.Sprint(target.Port)) + ";\n}"
		if strings.Count(text, block) != 1 {
			return errors.New("candidate_target_binding_invalid")
		}
	}
	seenMapLines := map[string]bool{}
	seenUpstreams := map[string]bool{}
	inMap := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "map ") {
			inMap = true
			continue
		}
		if inMap {
			if trimmed == "}" {
				inMap = false
				continue
			}
			if !expectedMapLines[trimmed] || seenMapLines[trimmed] {
				return errors.New("candidate_result_domain_invalid")
			}
			seenMapLines[trimmed] = true
			continue
		}
		if strings.HasPrefix(trimmed, "upstream ") {
			fields := strings.Fields(trimmed)
			if len(fields) != 3 || fields[2] != "{" || !allowed[fields[1]] || seenUpstreams[fields[1]] {
				return errors.New("candidate_result_domain_invalid")
			}
			seenUpstreams[fields[1]] = true
		}
	}
	if inMap || len(seenMapLines) != len(expectedMapLines) || len(seenUpstreams) != len(allowed) {
		return errors.New("candidate_result_domain_invalid")
	}
	wantProxy := plan.Targets.SelectedProxyMode == hostresources.ProxyModeOn
	if strings.Contains(lower, "proxy_protocol on;") != wantProxy || strings.Count(lower, "proxy_protocol on;") > 1 {
		return errors.New("proxy_protocol_mismatch")
	}
	return nil
}

func validGeneratedMapIDV2(value string) bool {
	if !(strings.HasPrefix(value, "up_") || strings.HasPrefix(value, "reject_")) || len(value) != len("reject_")+24 && len(value) != len("up_")+24 {
		return false
	}
	for _, character := range value[strings.IndexByte(value, '_')+1:] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func validateSNIPlanShapeV2(plan FrontingStrategyPlanV2, now time.Time) error {
	if plan.Validate() != nil || plan.ExpiresAt <= now.UTC().Unix() || plan.Strategy.Desired != StrategySNIPreread || plan.Strategy.Selected != StrategySNIPreread ||
		plan.Strategy.Actual != FrontingActualNotAppliedV2 || plan.Safety.Projection != plan.Strategy || len(plan.Safety.Blocks) != 0 || len(plan.Safety.ReasonCodes) != 0 ||
		len(plan.Selectors.Tuples) == 0 || plan.Selectors.Validate() != nil || selectorSetHasALPNV2(plan.Selectors) || plan.Selectors.Default.Policy == SelectorDefaultNonTLSFixedTarget ||
		len(plan.Selectors.TargetRevisions) == 0 || len(plan.Selectors.TargetRevisions) > MaxFixedTargetsV1 ||
		plan.Targets.SelectedProxyMode != hostresources.ProxyModeOff && plan.Targets.SelectedProxyMode != hostresources.ProxyModeOn {
		return errors.New("candidate_invalid")
	}
	return nil
}
