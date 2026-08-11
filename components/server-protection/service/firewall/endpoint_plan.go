package firewall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

type EndpointActionInput struct {
	Action      domain.AppliedActionV1
	SourceClass string
}

type normalizedEndpointAction struct {
	Action       domain.AppliedActionV1
	SourceClass  string
	Contribution EndpointContribution
}

type EndpointPlanInput struct {
	InputRevision  string
	Graph          protectionresources.SocketOwnershipGraph
	Resources      []hostresources.ProtectableResource
	Management     []hostresources.ManagementEndpointV1
	RecoveryPaths  []hostresources.RecoveryPathV1
	TrustedSources []string
	Actions        []EndpointActionInput
	RequireSSHKeep bool
	Now            time.Time
}

func BuildEndpointPlan(input EndpointPlanInput) FirewallPlan {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	inputRevision := strings.TrimSpace(input.InputRevision)
	if inputRevision == "" {
		inputRevision = EndpointInputRevision(input)
	}
	plan := FirewallPlan{Schema: FirewallPlanSchemaV2, Mode: ModeCoexistenceEndpointManaged, InputRevision: inputRevision, GraphRevision: input.Graph.Revision, OwnerObservationRevision: input.Graph.OwnerObservationRevision, Resources: append([]hostresources.ProtectableResource(nil), input.Resources...), Endpoints: []EndpointPolicy{}, ManagementExemptions: []ManagementExemption{}, Limits: DynamicSetLimits{MaxElements: DefaultDynamicSetSize, DefaultTTLSeconds: DefaultDynamicTTLSeconds, MaxTTLSeconds: MaximumDynamicTTLSeconds}, AllowTCPPorts: []int{}, AllowUDPPorts: []int{}, GraylistCIDRs: []string{}, StormLimits: []StormLimit{}, Warnings: []string{}, ExplicitOpen: []string{}, ReasonCodes: []string{}}
	if !exactHexRevision(inputRevision) {
		plan.ApplyBlocked = true
		plan.ReasonCodes = append(plan.ReasonCodes, "snapshot_input_revision_invalid")
	}
	actionScopeRevision := EndpointActionScopeRevision(plan.Resources)
	sort.Slice(plan.Resources, func(i, j int) bool { return plan.Resources[i].ID < plan.Resources[j].ID })
	managementByResource := make(map[string][]hostresources.ManagementEndpointV1)
	managementByKey := make(map[string][]hostresources.ManagementEndpointV1)
	for _, endpoint := range input.Management {
		managementByResource[endpoint.ResourceID] = append(managementByResource[endpoint.ResourceID], endpoint)
		key := hostresources.PublicEndpointKey{Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: hostresources.NormalizeListen(endpoint.Bind).Value, Port: endpoint.Port}
		managementByKey[endpointKeyString(key)] = append(managementByKey[endpointKeyString(key)], endpoint)
	}
	actions, actionReasons := normalizeActionInputs(input.Actions, actionScopeRevision, now, plan.Limits)
	if len(actionReasons) > 0 {
		plan.ApplyBlocked = true
		plan.ReasonCodes = append(plan.ReasonCodes, actionReasons...)
	}
	seenActionEndpoints := make(map[string]struct{})
	plannedKeys := make(map[string]struct{})
	for _, resource := range plan.Resources {
		strategy := baselineStrategy(resource)
		keys, complete := hostresources.DeterministicConfiguredEndpointKeys(resource)
		if !complete {
			plan.ApplyBlocked = true
			plan.ReasonCodes = append(plan.ReasonCodes, "endpoint_inventory_incomplete")
			continue
		}
		for _, key := range sortedBaselineKeys(keys) {
			plannedKeys[endpointKeyString(key)] = struct{}{}
			management := len(managementByResource[resource.ID]) > 0 || len(managementByKey[endpointKeyString(key)]) > 0
			policy := EndpointPolicy{EndpointRevision: configuredEndpointRevision(resource, key, strategy), Key: key, ResourceID: resource.ID, Owner: resource.Owner, OwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision, Strategy: strategy, Management: management, DesiredStatus: "MANAGED_ENDPOINT", SelectedStatus: "PLANNED", ActualStatus: "NOT_APPLIED", Contributions: []EndpointContribution{}}
			for _, action := range actions[policy.EndpointRevision] {
				if action.Action.ResourceID != resource.ID || action.Action.GraphRevision != actionScopeRevision || action.Action.ResourceRevision != EndpointActionResourceRevision(resource) || action.Action.ConfigurationRevision != resource.Capabilities.ConfigRevision {
					plan.ApplyBlocked = true
					plan.ReasonCodes = append(plan.ReasonCodes, "action_revision_stale")
					continue
				}
				policy.Contributions = append(policy.Contributions, action.Contribution)
				seenActionEndpoints[policy.EndpointRevision] = struct{}{}
			}
			policy.Contributions = collapseContributions(resource.ID+"\x00"+policy.EndpointRevision, policy.Contributions)
			if len(policy.Contributions) > plan.Limits.MaxElements {
				plan.ApplyBlocked = true
				plan.ReasonCodes = append(plan.ReasonCodes, "endpoint_contribution_limit_exceeded")
				policy.Contributions = policy.Contributions[:plan.Limits.MaxElements]
			}
			if management {
				for _, contribution := range policy.Contributions {
					guard := protectionpolicy.EvaluateManagementGuard(protectionpolicy.ManagementGuardInput{Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: resource.ID}, Subject: domain.SignalSubjectV2{Type: "prefix", Value: contribution.Subject}, EndpointKey: key, Management: input.Management, RecoveryPaths: input.RecoveryPaths, TrustedSources: input.TrustedSources, MayRestrictTraffic: true, Now: now})
					if !guard.ActionAllowed {
						plan.ApplyBlocked = true
						plan.ReasonCodes = append(plan.ReasonCodes, guard.ReasonCodes...)
						plan.ReasonCodes = append(plan.ReasonCodes, "management_recovery_guard_blocked")
					}
				}
			}
			if !exactHexRevision(resource.Capabilities.ConfigRevision) || key.Network == hostresources.NetworkUnknown || key.AddressFamily == hostresources.AddressFamilyUnknown || key.Port == 0 {
				plan.ApplyBlocked = true
				plan.ReasonCodes = append(plan.ReasonCodes, "endpoint_key_or_configuration_unknown")
			}
			plan.Endpoints = append(plan.Endpoints, policy)
		}
	}
	for _, endpoint := range input.Management {
		key := hostresources.PublicEndpointKey{Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: hostresources.NormalizeListen(endpoint.Bind).Value, Port: endpoint.Port}
		if _, exists := plannedKeys[endpointKeyString(key)]; exists {
			continue
		}
		resourceID := strings.TrimSpace(endpoint.ResourceID)
		if resourceID == "" {
			resourceID = endpoint.ID
		}
		plan.Endpoints = append(plan.Endpoints, EndpointPolicy{
			EndpointRevision: managementEndpointRevision(endpoint), Key: key, ResourceID: resourceID,
			Owner: endpoint.Owner, ConfigurationRevision: endpoint.ConfigurationRevision,
			Strategy: protectionresources.StrategyDirectGuarded, Management: true,
			DesiredStatus: "MANAGEMENT_KEEP", SelectedStatus: "PLANNED", ActualStatus: "NOT_APPLIED", Contributions: []EndpointContribution{},
		})
		plannedKeys[endpointKeyString(key)] = struct{}{}
	}
	for endpointRevision := range actions {
		if _, exists := seenActionEndpoints[endpointRevision]; !exists {
			plan.ApplyBlocked = true
			plan.ReasonCodes = append(plan.ReasonCodes, "action_endpoint_revision_stale")
		}
	}
	plan.ManagementExemptions = buildManagementExemptions(input.Management, input.RecoveryPaths, input.TrustedSources, now)
	for _, endpoint := range plan.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkTCP {
			plan.AllowTCPPorts = append(plan.AllowTCPPorts, int(endpoint.Key.Port))
		} else if endpoint.Key.Network == hostresources.NetworkUDP {
			plan.AllowUDPPorts = append(plan.AllowUDPPorts, int(endpoint.Key.Port))
		}
		for _, contribution := range endpoint.Contributions {
			if contribution.Intent == domain.IntentSoftGraylist {
				plan.GraylistCIDRs = append(plan.GraylistCIDRs, contribution.Subject)
			}
		}
	}
	plan.AllowTCPPorts = sortedUniqueInts(plan.AllowTCPPorts)
	plan.AllowUDPPorts = sortedUniqueInts(plan.AllowUDPPorts)
	plan.GraylistCIDRs = uniqueSorted(plan.GraylistCIDRs)
	plan.BaselineEligibility = evaluateFirewallBaselineEligibility(plan.Resources, plan.Endpoints, input.Management, input.RecoveryPaths, input.TrustedSources, input.Graph, input.RequireSSHKeep, now)
	if !plan.BaselineEligibility.CandidateEligible {
		plan.ApplyBlocked = true
		plan.ReasonCodes = append(plan.ReasonCodes, plan.BaselineEligibility.ReasonCodes...)
	}
	plan.ReasonCodes = uniqueSorted(plan.ReasonCodes)
	plan.Warnings = append(plan.Warnings, plan.ReasonCodes...)
	plan.Warnings = append(plan.Warnings, plan.BaselineEligibility.AdvisoryCodes...)
	plan.Warnings = append(plan.Warnings, plan.BaselineEligibility.MutationReasonCodes...)
	plan.Warnings = uniqueSorted(plan.Warnings)
	sort.Slice(plan.Endpoints, func(i, j int) bool {
		return endpointPolicySortKey(plan.Endpoints[i]) < endpointPolicySortKey(plan.Endpoints[j])
	})
	sort.Slice(plan.ManagementExemptions, func(i, j int) bool {
		return exemptionSortKey(plan.ManagementExemptions[i]) < exemptionSortKey(plan.ManagementExemptions[j])
	})
	plan.Revision = firewallPlanRevision(plan)
	return plan
}

// EndpointInputRevision binds the complete semantic planner input. Volatile
// observation timestamps are deliberately removed. Listener ownership is not
// part of the baseline revision; recovery freshness is checked again by the
// prepare/apply callers through EndpointPlanInput.InputRevision.
func EndpointInputRevision(input EndpointPlanInput) string {
	management := canonicalManagementEndpoints(input.Management)
	for index := range management {
		management[index].ObservedAt = 0
		management[index].ReasonCodes = append([]string(nil), management[index].ReasonCodes...)
		sort.Strings(management[index].ReasonCodes)
	}
	sort.Slice(management, func(i, j int) bool { return revisionSortKey(management[i]) < revisionSortKey(management[j]) })
	recovery := append([]hostresources.RecoveryPathV1(nil), input.RecoveryPaths...)
	for index := range recovery {
		recovery[index].ReasonCodes = append([]string(nil), recovery[index].ReasonCodes...)
		sort.Strings(recovery[index].ReasonCodes)
	}
	sort.Slice(recovery, func(i, j int) bool { return revisionSortKey(recovery[i]) < revisionSortKey(recovery[j]) })
	trusted := append([]string(nil), input.TrustedSources...)
	sort.Strings(trusted)
	actions := append([]EndpointActionInput(nil), input.Actions...)
	sort.Slice(actions, func(i, j int) bool { return revisionSortKey(actions[i]) < revisionSortKey(actions[j]) })
	return hostresources.Revision(struct {
		Schema     string
		Resources  []BaselineResourceBinding
		Management []hostresources.ManagementEndpointV1
		Recovery   []hostresources.RecoveryPathV1
		Trusted    []string
		Actions    []EndpointActionInput
		RequireSSH bool
	}{FirewallPlanSchemaV2, CanonicalPlanResources(input.Resources), management, recovery, trusted, actions, input.RequireSSHKeep})
}

// CanonicalPlanResources returns the resource facts used in revision hashes.
// It preserves all semantic configuration while removing observation clocks
// and listener-deployment ownership proof, which is advisory for the additive
// baseline, then normalizes collection order.
type BaselineResourceBinding struct {
	ID                    string
	Kind                  string
	Protocol              string
	Listen                string
	Port                  int
	Public                bool
	ConfigurationRevision string
	ListenIntent          hostresources.ConfiguredListenIntentV1
}

func CanonicalPlanResources(values []hostresources.ProtectableResource) []BaselineResourceBinding {
	result := make([]BaselineResourceBinding, 0, len(values))
	for _, value := range values {
		result = append(result, BaselineResourceBinding{
			ID: value.ID, Kind: value.Kind, Protocol: value.Protocol,
			Listen: hostresources.NormalizeListen(value.Listen).Value, Port: value.Port, Public: value.Public,
			ConfigurationRevision: value.Capabilities.ConfigRevision, ListenIntent: value.ListenIntent,
		})
	}
	sort.Slice(result, func(i, j int) bool { return revisionSortKey(result[i]) < revisionSortKey(result[j]) })
	return result
}

func EndpointActionScopeRevision(resources []hostresources.ProtectableResource) string {
	return hostresources.Revision(struct {
		Schema    string
		Resources []BaselineResourceBinding
	}{FirewallPlanSchemaV2, CanonicalPlanResources(resources)})
}

func EndpointActionResourceRevision(resource hostresources.ProtectableResource) string {
	return hostresources.Revision(CanonicalPlanResources([]hostresources.ProtectableResource{resource})[0])
}

func canonicalManagementEndpoints(values []hostresources.ManagementEndpointV1) []hostresources.ManagementEndpointV1 {
	result := append([]hostresources.ManagementEndpointV1(nil), values...)
	for index := range result {
		result[index].Owner = ""
		result[index].ObservedAt = 0
		result[index].ReasonCodes = nil
	}
	return result
}

func revisionSortKey(value any) string {
	payload, _ := json.Marshal(value)
	return string(payload)
}

func normalizeActionInputs(values []EndpointActionInput, actionScopeRevision string, now time.Time, limits DynamicSetLimits) (map[string][]normalizedEndpointAction, []string) {
	result := make(map[string][]normalizedEndpointAction)
	reasons := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range values {
		action := value.Action
		if !action.ExpiresAt.After(now) {
			continue
		}
		if err := action.Validate(now); err != nil || action.State != domain.ActionPlanned && action.State != domain.ActionVerified || action.GraphRevision != actionScopeRevision || action.ExpiresAt.After(now.Add(time.Duration(limits.MaxTTLSeconds)*time.Second)) || value.SourceClass != "native" && value.SourceClass != "external" {
			reasons = append(reasons, "action_contract_invalid")
			continue
		}
		subject := canonicalActionSubject(action.Subject)
		if subject == "" || !materializedIntent(action.ResolvedIntent) {
			reasons = append(reasons, "action_contract_invalid")
			continue
		}
		if _, exists := seen[action.ActionID]; exists {
			continue
		}
		seen[action.ActionID] = struct{}{}
		ttl := int(action.ExpiresAt.Sub(now) / time.Second)
		if ttl < 1 {
			ttl = 1
		}
		if ttl > limits.MaxTTLSeconds {
			ttl = limits.MaxTTLSeconds
		}
		contribution := EndpointContribution{ContributionID: action.ActionID, ActionID: action.ActionID, ActionIDs: []string{action.ActionID}, DecisionID: action.DecisionID, DecisionIDs: []string{action.DecisionID}, RefCount: 1, Subject: subject, Intent: action.ResolvedIntent, ExpiresAt: action.ExpiresAt.UTC().Unix(), TTLSeconds: ttl, SourceClass: value.SourceClass, SourceClasses: []string{value.SourceClass}}
		result[action.EndpointRevision] = append(result[action.EndpointRevision], normalizedEndpointAction{Action: action, SourceClass: value.SourceClass, Contribution: contribution})
	}
	for endpointRevision := range result {
		sort.Slice(result[endpointRevision], func(i, j int) bool {
			return result[endpointRevision][i].Action.ActionID < result[endpointRevision][j].Action.ActionID
		})
	}
	return result, uniqueSorted(reasons)
}

func collapseContributions(resourceID string, values []EndpointContribution) []EndpointContribution {
	groups := make(map[string]EndpointContribution)
	actions := make(map[string]map[string]struct{})
	decisions := make(map[string]map[string]struct{})
	sources := make(map[string]map[string]struct{})
	for _, value := range values {
		key := value.Subject + "\x00" + string(value.Intent)
		group := groups[key]
		if group.Subject == "" {
			payload, _ := json.Marshal(struct{ ResourceID, Subject, Intent string }{resourceID, value.Subject, string(value.Intent)})
			sum := sha256.Sum256(payload)
			group = value
			group.ContributionID = hex.EncodeToString(sum[:])
			group.ActionID = ""
			group.ActionIDs = nil
			group.DecisionID = ""
			group.DecisionIDs = nil
			group.RefCount = 0
			actions[key] = make(map[string]struct{})
			decisions[key] = make(map[string]struct{})
			sources[key] = make(map[string]struct{})
		}
		if value.DecisionID != "" {
			decisions[key][value.DecisionID] = struct{}{}
		}
		for _, actionID := range append([]string{value.ActionID}, value.ActionIDs...) {
			if actionID != "" {
				actions[key][actionID] = struct{}{}
			}
		}
		if value.SourceClass != "" {
			sources[key][value.SourceClass] = struct{}{}
		}
		if value.ExpiresAt > group.ExpiresAt {
			group.ExpiresAt = value.ExpiresAt
		}
		if value.TTLSeconds > group.TTLSeconds {
			group.TTLSeconds = value.TTLSeconds
		}
		groups[key] = group
	}
	result := make([]EndpointContribution, 0, len(groups))
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		for actionID := range actions[key] {
			group.ActionIDs = append(group.ActionIDs, actionID)
		}
		for decisionID := range decisions[key] {
			group.DecisionIDs = append(group.DecisionIDs, decisionID)
		}
		for sourceClass := range sources[key] {
			group.SourceClasses = append(group.SourceClasses, sourceClass)
		}
		sort.Strings(group.SourceClasses)
		group.SourceClass = ""
		if len(group.SourceClasses) > 0 {
			group.SourceClass = group.SourceClasses[0]
		}
		sort.Strings(group.DecisionIDs)
		sort.Strings(group.ActionIDs)
		group.RefCount = len(group.ActionIDs)
		if group.RefCount == 0 {
			group.RefCount = 1
		}
		result = append(result, group)
	}
	return result
}

func materializedIntent(value domain.ResponseIntent) bool {
	return value == domain.IntentSoftGraylist || value == domain.IntentRateLimit || value == domain.IntentTemporaryQuarantine || value == domain.IntentTemporaryBlock
}

func canonicalActionSubject(value domain.SignalSubjectV2) string {
	switch value.Type {
	case "ip":
		address, err := netip.ParseAddr(value.Value)
		if err != nil || address.Unmap().String() != value.Value {
			return ""
		}
		bits := 128
		if address.Is4() {
			bits = 32
		}
		return netip.PrefixFrom(address, bits).String()
	case "prefix":
		prefix, err := netip.ParsePrefix(value.Value)
		if err != nil || prefix.Masked().String() != value.Value {
			return ""
		}
		return prefix.String()
	default:
		return ""
	}
}

func buildManagementExemptions(endpoints []hostresources.ManagementEndpointV1, paths []hostresources.RecoveryPathV1, trusted []string, now time.Time) []ManagementExemption {
	result := make([]ManagementExemption, 0)
	for _, endpoint := range endpoints {
		key := hostresources.PublicEndpointKey{Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: hostresources.NormalizeListen(endpoint.Bind).Value, Port: endpoint.Port}
		if endpoint.ConfigurationRevision == "" || endpoint.ConfidenceBP <= 0 || key.Network == hostresources.NetworkUnknown || key.AddressFamily == hostresources.AddressFamilyUnknown || key.Port == 0 {
			continue
		}
		for _, path := range paths {
			if path.EndpointID != endpoint.ID || !strings.EqualFold(path.Kind, string(endpoint.ServiceKind)) || path.ConfigurationRevision != endpoint.ConfigurationRevision || !hostresources.RecoveryPathFresh(path, now) || path.SourcePrefix == "" {
				continue
			}
			prefix, prefixErr := netip.ParsePrefix(path.SourcePrefix)
			if canonicalPrefix(path.SourcePrefix) != "" && prefixErr == nil && ((endpoint.Family == hostresources.AddressFamilyIPv4) == prefix.Addr().Is4()) {
				result = append(result, ManagementExemption{EndpointID: endpoint.ID, RecoveryPathID: path.ID, Key: key, SourcePrefix: path.SourcePrefix, ExpiresAt: path.ExpiresAt})
			}
		}
		for _, prefix := range trusted {
			parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix))
			if canonicalPrefix(prefix) != "" && err == nil && ((endpoint.Family == hostresources.AddressFamilyIPv4) == parsed.Addr().Is4()) {
				result = append(result, ManagementExemption{EndpointID: endpoint.ID, RecoveryPathID: "trusted-source", Key: key, SourcePrefix: prefix})
			}
		}
	}
	return dedupeExemptions(result)
}

func canonicalPrefix(value string) string {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil || prefix.Masked().String() != strings.TrimSpace(value) {
		return ""
	}
	return prefix.String()
}

func configuredEndpointRevision(resource hostresources.ProtectableResource, key hostresources.PublicEndpointKey, strategy protectionresources.Strategy) string {
	payload, _ := json.Marshal(struct {
		ResourceID, ConfigurationRevision, Strategy string
		Key                                         hostresources.PublicEndpointKey
	}{resource.ID, resource.Capabilities.ConfigRevision, string(strategy), key})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func managementEndpointRevision(endpoint hostresources.ManagementEndpointV1) string {
	key := hostresources.PublicEndpointKey{Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: hostresources.NormalizeListen(endpoint.Bind).Value, Port: endpoint.Port}
	payload, _ := json.Marshal(struct {
		ID, ConfigurationRevision string
		Key                       hostresources.PublicEndpointKey
	}{endpoint.ID, endpoint.ConfigurationRevision, key})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func baselineStrategy(resource hostresources.ProtectableResource) protectionresources.Strategy {
	kind := strings.ToLower(strings.TrimSpace(resource.Kind))
	if kind == "tun" || kind == "redirect" || kind == "tproxy" {
		return protectionresources.StrategyInterceptionGuard
	}
	if hostresources.NetworkForProtocol(resource.Protocol) == hostresources.NetworkUDP {
		return protectionresources.StrategyUDPQUICDirectGuarded
	}
	if kind == "socks" || kind == "http" || kind == "mixed" {
		return protectionresources.StrategyLocalProxyGuard
	}
	return protectionresources.StrategyDirectGuarded
}

func endpointKeyString(key hostresources.PublicEndpointKey) string {
	return string(key.Network) + "\x00" + string(key.AddressFamily) + "\x00" + hostresources.NormalizeListen(key.BindAddress).Value + "\x00" + strconv.Itoa(int(key.Port))
}

func endpointPolicySortKey(value EndpointPolicy) string {
	return endpointKeyString(value.Key) + "\x00" + value.ResourceID
}
func exemptionSortKey(value ManagementExemption) string {
	return endpointKeyString(value.Key) + "\x00" + value.SourcePrefix + "\x00" + value.RecoveryPathID
}

func dedupeExemptions(values []ManagementExemption) []ManagementExemption {
	seen := make(map[string]struct{}, len(values))
	result := make([]ManagementExemption, 0, len(values))
	for _, value := range values {
		key := endpointKeyString(value.Key) + "\x00" + value.SourcePrefix
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return exemptionSortKey(result[i]) < exemptionSortKey(result[j]) })
	return result
}

func sortedUniqueInts(values []int) []int {
	sort.Ints(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func preflightEndpointPlan(plan FirewallPlan) error {
	if plan.Schema != FirewallPlanSchemaV2 || !exactHexRevision(plan.InputRevision) ||
		plan.BaselineEligibility.Kind != FirewallBaselineEligibilityKind || !validBaselineEligibility(plan.BaselineEligibility) {
		return ErrPlanRevision
	}
	if plan.ApplyBlocked || len(plan.ReasonCodes) != 0 {
		return fmt.Errorf("%w: endpoint plan is blocked", ErrUnsafeResource)
	}
	if !plan.BaselineEligibility.CandidateEligible || !plan.BaselineEligibility.MutationReady ||
		!plan.BaselineEligibility.EndpointInventoryComplete || !plan.BaselineEligibility.ManagementPreserved ||
		!plan.BaselineEligibility.ExactRevisions || !plan.BaselineEligibility.ManagedTableOnly || !plan.BaselineEligibility.NoForeignMutation {
		return fmt.Errorf("%w: firewall baseline eligibility is incomplete", ErrUnsafeResource)
	}
	if plan.Limits.MaxElements < 1 || plan.Limits.MaxElements > DefaultDynamicSetSize || plan.Limits.DefaultTTLSeconds < 1 || plan.Limits.DefaultTTLSeconds > plan.Limits.MaxTTLSeconds || plan.Limits.MaxTTLSeconds > MaximumDynamicTTLSeconds {
		return fmt.Errorf("%w: dynamic set bounds are invalid", ErrUnsafeResource)
	}
	if len(plan.Endpoints) == 0 || len(plan.Endpoints) > 4096 {
		return fmt.Errorf("%w: exact endpoint inventory is empty or exceeds its bound", ErrUnsafeResource)
	}
	seen := make(map[string]struct{}, len(plan.Endpoints))
	for _, endpoint := range plan.Endpoints {
		key := endpointPolicySortKey(endpoint)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: endpoint policy is duplicated", ErrUnsafeResource)
		}
		seen[key] = struct{}{}
		if endpoint.ResourceID == "" || !exactHexRevision(endpoint.EndpointRevision) || !exactHexRevision(endpoint.ConfigurationRevision) || endpoint.ActualStatus != "NOT_APPLIED" || endpoint.Key.Port == 0 {
			return fmt.Errorf("%w: endpoint identity or configuration revision is incomplete", ErrUnsafeResource)
		}
		if endpoint.Key.Network != hostresources.NetworkTCP && endpoint.Key.Network != hostresources.NetworkUDP || endpoint.Key.AddressFamily != hostresources.AddressFamilyIPv4 && endpoint.Key.AddressFamily != hostresources.AddressFamilyIPv6 || endpointMatch(endpoint.Key, "") == "" {
			return fmt.Errorf("%w: endpoint key is invalid", ErrUnsafeResource)
		}
		if endpoint.Strategy == protectionresources.StrategyUnclassified || endpoint.Strategy == "" {
			return fmt.Errorf("%w: endpoint strategy is unavailable", ErrUnsafeResource)
		}
		if endpoint.UDPFlowPolicy != nil {
			policy := endpoint.UDPFlowPolicy
			if endpoint.Key.Network != hostresources.NetworkUDP || policy.ResourceID != endpoint.ResourceID ||
				policy.EndpointID != endpoint.EndpointRevision || policy.AddressFamily != endpoint.Key.AddressFamily ||
				policy.Protocol != hostresources.NetworkUDP || policy.Validate() != nil {
				return fmt.Errorf("%w: UDP flow policy is not bound to its exact endpoint", ErrUnsafeResource)
			}
		}
		if len(endpoint.Contributions) > plan.Limits.MaxElements {
			return fmt.Errorf("%w: endpoint contribution set exceeds its cap", ErrUnsafeResource)
		}
		for _, contribution := range endpoint.Contributions {
			prefix, prefixErr := netip.ParsePrefix(contribution.Subject)
			familyMatches := prefixErr == nil && ((endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv4) == prefix.Addr().Is4())
			if contribution.ContributionID == "" || !materializedIntent(contribution.Intent) || canonicalPrefix(contribution.Subject) == "" || !familyMatches || contribution.TTLSeconds < 1 || contribution.TTLSeconds > plan.Limits.MaxTTLSeconds || contribution.RefCount < 1 || contribution.RefCount > plan.Limits.MaxElements || len(contribution.ActionIDs) != contribution.RefCount || len(contribution.DecisionIDs) > contribution.RefCount {
				return fmt.Errorf("%w: endpoint contribution is invalid", ErrUnsafeResource)
			}
			for _, id := range append(append([]string(nil), contribution.ActionIDs...), contribution.DecisionIDs...) {
				if len(id) != 64 || !hexRevision(id) {
					return fmt.Errorf("%w: endpoint contribution identity is invalid", ErrUnsafeResource)
				}
			}
		}
	}
	for _, exemption := range plan.ManagementExemptions {
		if exemption.EndpointID == "" || exemption.RecoveryPathID == "" || canonicalPrefix(exemption.SourcePrefix) == "" || endpointMatch(exemption.Key, exemption.SourcePrefix) == "" || (exemption.RecoveryPathID != "trusted-source" && exemption.ExpiresAt <= 0) {
			return fmt.Errorf("%w: management exemption is not exact", ErrUnsafeResource)
		}
	}
	candidate := RenderManagedNFT(plan)
	lower := strings.ToLower(candidate)
	for _, forbidden := range []string{"flush ruleset", "table ip ", "table ip6 ", "docker", "iptables", "firewalld", "include ", "define "} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("%w: candidate contains forbidden global or unmanaged semantics", ErrUnsafeResource)
		}
	}
	return nil
}

func hexRevision(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			continue
		}
		return false
	}
	return true
}

func exactHexRevision(value string) bool {
	return len(value) == 64 && hexRevision(value)
}
