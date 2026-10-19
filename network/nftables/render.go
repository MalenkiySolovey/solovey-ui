package nftables

import (
	"strconv"
	"strings"
	"time"

	networkplan "github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// RenderProtection materializes typed decisions only. Mutation still requires
// accepted candidate identity and the existing privileged owner fence.
func RenderProtection(intent networkplan.ProtectionEnforcementIntent, managed bool, at time.Time) string {
	intent = networkplan.CanonicalProtectionIntent(intent)
	if intent.Mode == networkplan.ProtectionEndpointMode {
		return renderEndpointManagedNFTAt(intent, managed, at)
	}
	return renderLegacyProtection(intent, managed)
}

// DeleteOwnedTableCandidate is used only with fresh proven ownership and the
// expected exact state in the existing typed rollback boundary.
func DeleteOwnedTableCandidate() []byte {
	return []byte("delete table " + networkplan.ProtectionTableCLI + "\n")
}

func renderLegacyProtection(plan networkplan.ProtectionEnforcementIntent, managed bool) string {
	var output strings.Builder
	output.WriteString("table inet solovey_protection {\n")
	if managed {
		output.WriteString("  comment \"solovey-revision:")
		output.WriteString(plan.ArtifactRevision)
		output.WriteString("\"\n")
	}
	writePortSet(&output, "solovey_allow_tcp_ports", plan.AllowTCPPorts)
	writePortSet(&output, "solovey_allow_udp_ports", plan.AllowUDPPorts)
	writeCIDRSets(&output, plan.GraylistCIDRs)
	output.WriteString("  chain solovey_input_precheck {\n")
	output.WriteString("    type filter hook input priority filter; policy accept;\n")
	if len(plan.AllowTCPPorts) > 0 {
		output.WriteString("    meta l4proto tcp tcp dport @solovey_allow_tcp_ports counter accept\n")
	}
	if len(plan.AllowUDPPorts) > 0 {
		output.WriteString("    meta l4proto udp udp dport @solovey_allow_udp_ports counter accept\n")
	}
	output.WriteString("  }\n")
	output.WriteString("  chain solovey_tcp_public {\n    counter accept\n  }\n")
	output.WriteString("  chain solovey_udp_public {\n    counter accept\n  }\n")
	output.WriteString("}\n")
	return output.String()
}

func writePortSet(output *strings.Builder, name string, ports []int) {
	output.WriteString("  set ")
	output.WriteString(name)
	output.WriteString(" {\n    type inet_service\n    flags interval\n    elements = { ")
	for index, port := range ports {
		if index > 0 {
			output.WriteString(", ")
		}
		output.WriteString(strconv.Itoa(port))
	}
	output.WriteString(" }\n  }\n")
}

func writeCIDRSets(output *strings.Builder, cidrs []string) {
	ipv4 := make([]string, 0)
	ipv6 := make([]string, 0)
	for _, cidr := range cidrs {
		if strings.Contains(cidr, ":") {
			ipv6 = append(ipv6, cidr)
		} else {
			ipv4 = append(ipv4, cidr)
		}
	}
	writeAddressSet(output, "solovey_graylist4", "ipv4_addr", ipv4)
	writeAddressSet(output, "solovey_graylist6", "ipv6_addr", ipv6)
}

func writeAddressSet(output *strings.Builder, name, addressType string, values []string) {
	output.WriteString("  set ")
	output.WriteString(name)
	output.WriteString(" {\n    type ")
	output.WriteString(addressType)
	output.WriteString("\n    flags interval\n    elements = { ")
	for index, value := range values {
		if index > 0 {
			output.WriteString(", ")
		}
		output.WriteString(value)
	}
	output.WriteString(" }\n  }\n")
}
