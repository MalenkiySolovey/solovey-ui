package firewall

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

type nftTimedElement struct {
	Prefix string
	TTL    int64
}

func renderEndpointManagedNFT(plan FirewallPlan, managed bool) string {
	var output strings.Builder
	output.WriteString("table inet solovey_protection {\n")
	if managed {
		output.WriteString("  comment \"solovey-revision:")
		output.WriteString(plan.Revision)
		output.WriteString("\"\n")
	}
	for _, endpoint := range plan.Endpoints {
		writeEndpointSets(&output, endpoint, plan)
	}
	output.WriteString("  chain solovey_input {\n")
	output.WriteString("    type filter hook input priority -5; policy accept;\n")
	// Recovery/trusted exceptions are exact endpoint+source rules and precede
	// generic endpoint enforcement. They never exempt unrelated VPN traffic.
	for _, exemption := range plan.ManagementExemptions {
		if rule := endpointMatch(exemption.Key, exemption.SourcePrefix); rule != "" {
			output.WriteString("    ")
			output.WriteString(rule)
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
			output.WriteString(shortRevision(endpoint.EndpointRevision))
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

func writeEndpointSets(output *strings.Builder, endpoint EndpointPolicy, plan FirewallPlan) {
	grouped := endpointElements(endpoint, plan)
	for _, intent := range []domain.ResponseIntent{domain.IntentSoftGraylist, domain.IntentRateLimit, domain.IntentTemporaryQuarantine, domain.IntentTemporaryBlock} {
		elements := grouped[intent]
		if len(elements) == 0 {
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
		output.WriteString("    elements = { ")
		for index, element := range elements {
			if index > 0 {
				output.WriteString(", ")
			}
			output.WriteString(element.Prefix)
			output.WriteString(" timeout ")
			output.WriteString(strconv.FormatInt(element.TTL, 10))
			output.WriteString("s")
		}
		output.WriteString(" }\n")
		output.WriteString("  }\n")
	}
}

func writeEndpointChain(output *strings.Builder, endpoint EndpointPolicy, plan FirewallPlan) {
	output.WriteString("  chain solovey_endpoint_")
	output.WriteString(shortRevision(endpoint.EndpointRevision))
	output.WriteString(" {\n")
	familyKeyword := "ip"
	if endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv6 {
		familyKeyword = "ip6"
	}
	sets := endpointElements(endpoint, plan)
	if endpoint.UDPFlowPolicy != nil {
		// The typed profile is product-owned and bounded. ICMP/ICMPv6 never
		// enter this UDP endpoint chain, so path-MTU and control traffic remain
		// accepted by the containing input chain's coexistence policy.
		output.WriteString("    ct state new limit rate over 200/second burst 400 packets counter drop\n")
	}
	if len(sets[domain.IntentSoftGraylist]) > 0 {
		fmt.Fprintf(output, "    %s saddr @%s limit rate over 5/second burst 10 packets counter drop\n", familyKeyword, endpointSetName(domain.IntentSoftGraylist, endpoint))
	}
	if len(sets[domain.IntentRateLimit]) > 0 {
		fmt.Fprintf(output, "    %s saddr @%s limit rate over 20/second burst 40 packets counter drop\n", familyKeyword, endpointSetName(domain.IntentRateLimit, endpoint))
	}
	if len(sets[domain.IntentTemporaryQuarantine]) > 0 {
		fmt.Fprintf(output, "    %s saddr @%s limit rate over 2/second burst 4 packets counter drop\n", familyKeyword, endpointSetName(domain.IntentTemporaryQuarantine, endpoint))
	}
	if len(sets[domain.IntentTemporaryBlock]) > 0 {
		fmt.Fprintf(output, "    %s saddr @%s counter drop\n", familyKeyword, endpointSetName(domain.IntentTemporaryBlock, endpoint))
	}
	output.WriteString("    counter accept\n")
	output.WriteString("  }\n")
}

func endpointElements(endpoint EndpointPolicy, plan FirewallPlan) map[domain.ResponseIntent][]nftTimedElement {
	result := make(map[domain.ResponseIntent][]nftTimedElement)
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
		result[contribution.Intent] = append(result[contribution.Intent], nftTimedElement{Prefix: contribution.Subject, TTL: ttl})
	}
	for intent := range result {
		sort.Slice(result[intent], func(i, j int) bool {
			return result[intent][i].Prefix+"\x00"+strconv.FormatInt(result[intent][i].TTL, 10) < result[intent][j].Prefix+"\x00"+strconv.FormatInt(result[intent][j].TTL, 10)
		})
	}
	return result
}

func endpointSetName(intent domain.ResponseIntent, endpoint EndpointPolicy) string {
	prefix := map[domain.ResponseIntent]string{domain.IntentSoftGraylist: "gray", domain.IntentRateLimit: "rate", domain.IntentTemporaryQuarantine: "quarantine", domain.IntentTemporaryBlock: "block"}[intent]
	family := "4"
	if endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv6 {
		family = "6"
	}
	return "solovey_" + prefix + family + "_" + shortRevision(endpoint.EndpointRevision)
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
