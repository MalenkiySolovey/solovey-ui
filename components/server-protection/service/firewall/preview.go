package firewall

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func Preview(plan FirewallPlan, options PreviewOptions) FirewallPreview {
	operatingSystem := strings.ToLower(strings.TrimSpace(options.OperatingSystem))
	if operatingSystem == "" {
		operatingSystem = runtime.GOOS
	}
	preview := FirewallPreview{
		Revision: plan.Revision, InputRevision: plan.InputRevision, Backend: "preview_only", WouldKeep: []string{}, WouldOpen: append([]string(nil), plan.ExplicitOpen...),
		WouldWarn: append([]string(nil), plan.Warnings...), WouldBlock: []string{}, Warnings: append([]string(nil), plan.Warnings...),
		ProtectedKeep: append([]hostresources.ProtectableResource(nil), plan.Resources...),
	}
	for _, resource := range plan.Resources {
		if resource.Port < 1 || resource.Port > 65535 {
			continue
		}
		preview.WouldKeep = append(preview.WouldKeep, fmt.Sprintf("%s %s %s:%d", resource.ID, socketProtocol(resource.Protocol), resource.Listen, resource.Port))
	}
	if !containsPort(plan.AllowTCPPorts, 22) {
		preview.WouldWarn = append(preview.WouldWarn, "SSH is not represented in the keep policy")
	}
	sort.Strings(preview.WouldKeep)
	sort.Strings(preview.WouldOpen)
	preview.WouldWarn = uniqueSorted(preview.WouldWarn)
	preview.Warnings = uniqueSorted(preview.Warnings)
	if operatingSystem != "linux" {
		preview.Backend = "unsupported"
		preview.WouldWarn = uniqueSorted(append(preview.WouldWarn, "firewall preview script is unavailable on "+operatingSystem))
		preview.Warnings = uniqueSorted(append(preview.Warnings, "non-Linux environment: inventory only"))
		return preview
	}
	if options.IncludeGeneratedNFT {
		preview.GeneratedNFT = RenderNFTPreview(plan)
	}
	return preview
}

func RenderNFTPreview(plan FirewallPlan) string {
	return renderNFT(plan, false)
}

// RenderManagedNFT is the exact apply artifact. It remains non-enforcing by
// default; a future hard-block policy must use a separate advanced-confirmed
// model rather than editing this text through the API.
func RenderManagedNFT(plan FirewallPlan) string {
	return renderNFT(plan, true)
}

func renderNFT(plan FirewallPlan, managed bool) string {
	if plan.Mode == ModeCoexistenceEndpointManaged {
		return renderEndpointManagedNFT(plan, managed)
	}
	var output strings.Builder
	output.WriteString("table inet solovey_protection {\n")
	if managed {
		output.WriteString("  comment \"solovey-revision:")
		output.WriteString(plan.Revision)
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

func containsPort(values []int, port int) bool {
	index := sort.SearchInts(values, port)
	return index < len(values) && values[index] == port
}
