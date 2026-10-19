package nftables

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	networkplan "github.com/MalenkiySolovey/solovey-ui/network/planner"
)

type nftTimedElement struct {
	Prefix    string
	TTL       int64
	ExpiresAt int64
}

// An observation-time projection retains static topology. Only timed members
// already expired under the committed absolute deadline can disappear.
func renderEndpointManagedNFTAt(plan networkplan.ProtectionEnforcementIntent, managed bool, at time.Time) string {
	var output strings.Builder
	output.WriteString("table inet solovey_protection {\n")
	if managed {
		output.WriteString("  comment \"solovey-revision:")
		output.WriteString(plan.ArtifactRevision)
		output.WriteString("\"\n")
	}
	for _, endpoint := range plan.Endpoints {
		writeEndpointSetsAt(&output, endpoint, plan, at)
	}
	output.WriteString("  chain solovey_input {\n")
	output.WriteString("    type filter hook input priority -5; policy accept;\n")
	// Recovery/trusted exceptions are exact endpoint+source rules and precede
	// generic endpoint enforcement. They never exempt unrelated VPN traffic.
	for _, exemption := range plan.ManagementExemptions {
		if rule := endpointMatch(exemption.Key, exemption.SourcePrefix); rule != "" {
			output.WriteString("    ")
			output.WriteString(rule)
			if exemption.RecoveryPathID != "trusted-source" {
				output.WriteString(" meta time < ")
				output.WriteString(strconv.FormatInt(exemption.ExpiresAt, 10))
			}
			output.WriteString(" counter accept\n")
		}
	}
	output.WriteString("    iifname \"lo\" counter accept\n")
	output.WriteString("    ct state established,related counter accept\n")
	for _, endpoint := range plan.Endpoints {
		if rule := endpointMatch(endpoint.Key, ""); rule != "" {
			output.WriteString("    ")
			output.WriteString(rule)
			output.WriteString(" jump solovey_endpoint_")
			output.WriteString(shortRevision(endpoint.Binding.EndpointRevision))
			output.WriteString("\n")
		}
	}
	output.WriteString("  }\n")
	for _, endpoint := range plan.Endpoints {
		writeEndpointChain(&output, endpoint, plan)
	}
	output.WriteString("}\n")
	return output.String()
}

func writeEndpointSetsAt(output *strings.Builder, endpoint networkplan.ProtectionEndpoint, plan networkplan.ProtectionEnforcementIntent, at time.Time) {
	grouped := endpointElements(endpoint, plan)
	current := grouped
	if !at.IsZero() {
		active := endpoint
		active.Contributions = nil
		for _, contribution := range endpoint.Contributions {
			if contribution.ExpiresAt > at.Unix() {
				active.Contributions = append(active.Contributions, contribution)
			}
		}
		current = endpointElements(active, plan)
	}
	for _, intent := range []networkplan.SetClass{networkplan.GraySet, networkplan.RateSet, networkplan.QuarantineSet, networkplan.BlockSet} {
		elements := current[intent]
		if len(grouped[intent]) == 0 {
			continue
		}
		name := endpointSetName(intent, endpoint)
		addressType := "ipv4_addr"
		if endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv6 {
			addressType = "ipv6_addr"
		}
		output.WriteString("  set ")
		output.WriteString(name)
		output.WriteString(" {\n")
		output.WriteString("    type ")
		output.WriteString(addressType)
		output.WriteString("\n")
		output.WriteString("    flags interval,timeout\n")
		output.WriteString("    size ")
		output.WriteString(strconv.Itoa(plan.Limits.MaxElements))
		output.WriteString("\n")
		output.WriteString("    timeout ")
		output.WriteString(strconv.Itoa(plan.Limits.DefaultTTLSeconds))
		output.WriteString("s\n")
		if len(elements) > 0 {
			output.WriteString("    elements = { ")
		}
		for index, element := range elements {
			if index > 0 {
				output.WriteString(", ")
			}
			output.WriteString(element.Prefix)
			output.WriteString(" timeout ")
			output.WriteString(strconv.FormatInt(element.TTL, 10))
			output.WriteString("s")
			if !at.IsZero() {
				remaining := time.Unix(element.ExpiresAt, 0).Sub(at).Milliseconds()
				output.WriteString(" expires ")
				output.WriteString(strconv.FormatInt(remaining, 10))
				output.WriteString("ms")
			}
		}
		if len(elements) > 0 {
			output.WriteString(" }\n")
		}
		output.WriteString("  }\n")
	}
}

func writeEndpointChain(output *strings.Builder, endpoint networkplan.ProtectionEndpoint, plan networkplan.ProtectionEnforcementIntent) {
	output.WriteString("  chain solovey_endpoint_")
	output.WriteString(shortRevision(endpoint.Binding.EndpointRevision))
	output.WriteString(" {\n")
	familyKeyword := "ip"
	if endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv6 {
		familyKeyword = "ip6"
	}
	sets := endpointElements(endpoint, plan)
	if endpoint.UDPNew != nil {
		// The typed profile is product-owned and bounded. ICMP/ICMPv6 never
		// enter this UDP endpoint chain, so path-MTU and control traffic remain
		// accepted by the containing input chain's coexistence policy.
		fmt.Fprintf(output, "    ct state new limit rate over %d/second burst %d packets counter drop\n", endpoint.UDPNew.Rate, endpoint.UDPNew.Burst)
	}
	if len(sets[networkplan.GraySet]) > 0 {
		fmt.Fprintf(output, "    %s saddr @%s limit rate over %d/second burst %d packets counter drop\n", familyKeyword, endpointSetName(networkplan.GraySet, endpoint), plan.Rates.Gray.Rate, plan.Rates.Gray.Burst)
	}
	if len(sets[networkplan.RateSet]) > 0 {
		fmt.Fprintf(output, "    %s saddr @%s limit rate over %d/second burst %d packets counter drop\n", familyKeyword, endpointSetName(networkplan.RateSet, endpoint), plan.Rates.Rate.Rate, plan.Rates.Rate.Burst)
	}
	if len(sets[networkplan.QuarantineSet]) > 0 {
		fmt.Fprintf(output, "    %s saddr @%s limit rate over %d/second burst %d packets counter drop\n", familyKeyword, endpointSetName(networkplan.QuarantineSet, endpoint), plan.Rates.Quarantine.Rate, plan.Rates.Quarantine.Burst)
	}
	if len(sets[networkplan.BlockSet]) > 0 {
		fmt.Fprintf(output, "    %s saddr @%s counter drop\n", familyKeyword, endpointSetName(networkplan.BlockSet, endpoint))
	}
	output.WriteString("    counter accept\n")
	output.WriteString("  }\n")
}

func endpointElements(endpoint networkplan.ProtectionEndpoint, plan networkplan.ProtectionEnforcementIntent) map[networkplan.SetClass][]nftTimedElement {
	result := make(map[networkplan.SetClass][]nftTimedElement)
	for _, contribution := range endpoint.Contributions {
		prefix, err := netip.ParsePrefix(contribution.Subject)
		if err != nil || prefix.Masked().String() != contribution.Subject {
			continue
		}
		if endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv4 && !prefix.Addr().Is4() || endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv6 && prefix.Addr().Is4() {
			continue
		}
		ttl := int64(contribution.TTLSeconds)
		if ttl < 1 {
			ttl = 1
		}
		if ttl > int64(plan.Limits.MaxTTLSeconds) {
			ttl = int64(plan.Limits.MaxTTLSeconds)
		}
		result[contribution.Class] = append(result[contribution.Class], nftTimedElement{Prefix: contribution.Subject, TTL: ttl, ExpiresAt: contribution.ExpiresAt})
	}
	for intent := range result {
		sort.Slice(result[intent], func(i, j int) bool {
			return result[intent][i].Prefix+"\x00"+strconv.FormatInt(result[intent][i].TTL, 10) < result[intent][j].Prefix+"\x00"+strconv.FormatInt(result[intent][j].TTL, 10)
		})
	}
	return result
}

func endpointSetName(intent networkplan.SetClass, endpoint networkplan.ProtectionEndpoint) string {
	prefix := string(intent)
	family := "4"
	if endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv6 {
		family = "6"
	}
	return "solovey_" + prefix + family + "_" + shortRevision(endpoint.Binding.EndpointRevision)
}

func endpointMatch(key hostresources.PublicEndpointKey, sourcePrefix string) string {
	if key.Network != hostresources.NetworkTCP && key.Network != hostresources.NetworkUDP || key.AddressFamily != hostresources.AddressFamilyIPv4 && key.AddressFamily != hostresources.AddressFamilyIPv6 || key.Port == 0 {
		return ""
	}
	parts := make([]string, 0, 6)
	if key.AddressFamily == hostresources.AddressFamilyIPv4 {
		parts = append(parts, "meta nfproto ipv4")
	} else {
		parts = append(parts, "meta nfproto ipv6")
	}
	familyKeyword := "ip"
	if key.AddressFamily == hostresources.AddressFamilyIPv6 {
		familyKeyword = "ip6"
	}
	if sourcePrefix != "" {
		prefix, err := netip.ParsePrefix(sourcePrefix)
		if err != nil || prefix.Masked().String() != sourcePrefix || (key.AddressFamily == hostresources.AddressFamilyIPv4) != prefix.Addr().Is4() {
			return ""
		}
		parts = append(parts, familyKeyword+" saddr "+sourcePrefix)
	}
	listen := hostresources.NormalizeListen(key.BindAddress)
	if !listen.Wildcard() {
		address, err := netip.ParseAddr(listen.Value)
		if err != nil || (key.AddressFamily == hostresources.AddressFamilyIPv4) != address.Is4() {
			return ""
		}
		parts = append(parts, familyKeyword+" daddr "+address.String())
	}
	parts = append(parts, "meta l4proto "+string(key.Network), string(key.Network)+" dport "+strconv.Itoa(int(key.Port)))
	return strings.Join(parts, " ")
}

func shortRevision(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 12 {
		return strings.Repeat("0", 12)
	}
	return value[:12]
}
