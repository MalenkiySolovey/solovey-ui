package firewall

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func BuildPlan(resources []hostresources.ProtectableResource, ports []protectionrepository.PortAllowlistModel, graylist []protectionrepository.GraylistModel) FirewallPlan {
	plan := FirewallPlan{
		Resources:     append([]hostresources.ProtectableResource(nil), resources...),
		AllowTCPPorts: []int{}, AllowUDPPorts: []int{}, GraylistCIDRs: []string{}, StormLimits: []StormLimit{}, Warnings: []string{}, ExplicitOpen: []string{},
	}
	sort.Slice(plan.Resources, func(i, j int) bool { return plan.Resources[i].ID < plan.Resources[j].ID })
	tcp := map[int]struct{}{}
	udp := map[int]struct{}{}
	for _, resource := range plan.Resources {
		if resource.Port < 1 || resource.Port > 65535 {
			plan.Warnings = append(plan.Warnings, "resource "+resource.ID+" has no valid listener port")
			continue
		}
		protocol, supported := classifySocketProtocol(resource.Protocol)
		if !supported {
			plan.Warnings = append(plan.Warnings, "resource "+resource.ID+" has unsupported listener protocol")
			continue
		}
		if protocol == "udp" {
			udp[resource.Port] = struct{}{}
		} else {
			tcp[resource.Port] = struct{}{}
		}
		for _, warning := range resource.Warnings {
			plan.Warnings = append(plan.Warnings, resource.ID+": "+warning)
		}
	}
	hasExplicitSSH := false
	for _, item := range ports {
		protocol := strings.ToLower(strings.TrimSpace(item.Protocol))
		if protocol != "tcp" && protocol != "udp" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("allowlist item %d has unsupported protocol", item.ID))
			continue
		}
		start, end := item.PortStart, item.PortEnd
		if start < 1 || end < start || end > 65535 {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("allowlist item %d has invalid port range", item.ID))
			continue
		}
		for port := start; port <= end; port++ {
			if protocol == "udp" {
				udp[port] = struct{}{}
			} else {
				tcp[port] = struct{}{}
			}
		}
		if protocol == "tcp" && (start <= 22 && end >= 22 || strings.Contains(strings.ToLower(item.Reason), "ssh")) {
			hasExplicitSSH = true
		}
		listen := hostresources.NormalizeListen(item.Listen).Value
		plan.ExplicitOpen = append(plan.ExplicitOpen, protocol+" "+listen+":"+portRange(start, end)+" ("+boundedReason(item.Reason)+")")
	}
	if !hasExplicitSSH {
		tcp[22] = struct{}{}
		plan.Warnings = append(plan.Warnings, "SSH listener is unknown; TCP port 22 is kept only as an unverified fallback, add an explicit keep entry")
	}
	plan.AllowTCPPorts = sortedPorts(tcp)
	plan.AllowUDPPorts = sortedPorts(udp)
	seenCIDR := map[string]struct{}{}
	for _, item := range graylist {
		if item.IPCIDR == "" {
			continue
		}
		if _, ok := seenCIDR[item.IPCIDR]; ok {
			continue
		}
		seenCIDR[item.IPCIDR] = struct{}{}
		plan.GraylistCIDRs = append(plan.GraylistCIDRs, item.IPCIDR)
	}
	sort.Strings(plan.GraylistCIDRs)
	sort.Strings(plan.ExplicitOpen)
	plan.Warnings = uniqueSorted(plan.Warnings)
	plan.Revision = firewallPlanRevision(plan)
	return plan
}

func firewallPlanRevision(plan FirewallPlan) string {
	return hostresources.Revision(struct {
		Schema      string
		Mode        string
		Input       string
		Eligibility firewallBaselineEligibilityBinding
		Resources   []BaselineResourceBinding
		Endpoints   []EndpointPolicy
		Management  []ManagementExemption
		Limits      DynamicSetLimits
		Blocked     bool
		Reasons     []string
		TCP         []int
		UDP         []int
		Graylist    []string
	}{plan.Schema, plan.Mode, plan.InputRevision, baselineEligibilityBinding(plan.BaselineEligibility), CanonicalPlanResources(plan.Resources), canonicalEndpointPolicies(plan.Endpoints), plan.ManagementExemptions, plan.Limits, plan.ApplyBlocked, plan.ReasonCodes, plan.AllowTCPPorts, plan.AllowUDPPorts, plan.GraylistCIDRs})
}

// AttachUDPFlowPolicy returns a complete managed-table candidate with one
// typed UDP contribution. TCP endpoint semantics remain byte-for-byte
// unchanged and the caller cannot add an endpoint absent from the baseline.
func AttachUDPFlowPolicy(plan FirewallPlan, endpointRevision string, policy UDPFlowPolicyV1) (FirewallPlan, error) {
	if policy.Validate() != nil {
		return FirewallPlan{}, fmt.Errorf("%w: UDP flow policy is invalid", ErrUnsafeResource)
	}
	// FirewallPlan owns slices and pointer fields. A value copy would let one
	// UDP contribution mutate the caller's baseline (and therefore leak into a
	// sibling network/family contribution).
	plan = cloneFirewallPlan(plan)
	matched := 0
	for index := range plan.Endpoints {
		if plan.Endpoints[index].EndpointRevision != endpointRevision {
			continue
		}
		if plan.Endpoints[index].Key.Network != hostresources.NetworkUDP || plan.Endpoints[index].ResourceID != policy.ResourceID ||
			plan.Endpoints[index].Key.AddressFamily != policy.AddressFamily {
			return FirewallPlan{}, fmt.Errorf("%w: UDP endpoint binding changed", ErrUnsafeResource)
		}
		copy := policy
		copy.EndpointID = endpointRevision
		copy.Revision = hostresources.Revision(udpFlowPolicyRevisionInput(copy))
		plan.Endpoints[index].UDPFlowPolicy = &copy
		matched++
	}
	if matched != 1 {
		return FirewallPlan{}, fmt.Errorf("%w: exact UDP endpoint is unavailable", ErrUnsafeResource)
	}
	plan.Revision = firewallPlanRevision(plan)
	return plan, nil
}

func canonicalEndpointPolicies(values []EndpointPolicy) []EndpointPolicy {
	result := append([]EndpointPolicy(nil), values...)
	for index := range result {
		result[index].Owner = ""
		result[index].OwnerRevision = ""
	}
	return result
}

func socketProtocol(value string) string {
	protocol, _ := classifySocketProtocol(value)
	return protocol
}

func classifySocketProtocol(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "udp", "quic":
		return "udp", true
	case "tcp", "stream", "http", "https", "tls":
		return "tcp", true
	default:
		return "", false
	}
}

func sortedPorts(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for port := range values {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}

func portRange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "-" + strconv.Itoa(end)
}

func boundedReason(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 64 {
		return value[:64]
	}
	if value == "" {
		return "explicit"
	}
	return value
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
