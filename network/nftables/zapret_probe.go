//go:build linux && zapretproof

package nftables

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// RenderZapretProbe is a compiler for the isolated namespace adapter only.
// The production broker does not admit this table, hook, or queue action.
func RenderZapretProbe(plan planner.ZapretPlan) (string, error) {
	if plan.Plan().Revision() == "" || plan.Queue() == 0 || plan.SocketMark() == 0 {
		return "", errors.New("no validated Zapret probe plan")
	}
	scope := plan.PacketScope()
	var text strings.Builder
	fmt.Fprintf(&text, "table inet %s {\n", planner.ZapretProbeTable)
	for _, network := range scope.Protocols {
		fmt.Fprintf(&text, "counter %s_seen {}\ncounter %s_captured {}\n", network, network)
	}
	text.WriteString("chain outbound {\ntype filter hook output priority 150; policy accept;\n")
	for _, network := range scope.Protocols {
		ports := scope.TCPPorts
		if network == "udp" {
			ports = scope.UDPPorts
		}
		values := make([]string, 0, len(ports))
		for _, port := range ports {
			values = append(values, strconv.Itoa(int(port)))
		}
		match := fmt.Sprintf("oifname \"lo\" ct direction original %s dport { %s }", network, strings.Join(values, ", "))
		fmt.Fprintf(&text, "%s counter name %s_seen\n", match, network)
		fmt.Fprintf(&text, "%s meta mark 0x%x ct original packets 1-12 counter name %s_captured queue num %d\n",
			match, plan.SocketMark(), network, plan.Queue())
	}
	text.WriteString("}\n}\n")
	return text.String(), nil
}
