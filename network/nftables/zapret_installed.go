package nftables

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// RenderInstalledZapret emits one create-only batch. The pass bits have no
// authority by themselves: the independent POSTROUTING guard checks them
// on the actual egress and stays attached when the nft table disappears.
func RenderInstalledZapret(plan planner.ZapretPlan) (string, error) {
	scope, err := plan.SocketScope()
	if err != nil || plan.Queue() == 0 || plan.RawPassMark() == 0 {
		return "", errors.New("missing installed Zapret allocation")
	}
	egress := scope.Egress()
	table := "inet " + planner.ZapretInstalledTable
	var lines strings.Builder
	fmt.Fprintf(&lines, "create table %s { comment %q; }\n", table, "solovey-r8b1:"+scope.PlanRevision())
	fmt.Fprintf(&lines, "add chain %s local_output { type filter hook output priority 150; policy accept; }\n", table)
	families := "{ ipv4, ipv6 }"
	if !egress.IPv6 {
		families = "ipv4"
	} else if !egress.IPv4 {
		families = "ipv6"
	}
	path := fmt.Sprintf("oif %d meta nfproto %s", egress.IfIndex, families)
	packet := plan.PacketScope()
	for _, protocol := range packet.Protocols {
		ports := packet.TCPPorts
		if protocol == intent.PacketProtocolUDP {
			ports = packet.UDPPorts
		}
		values := make([]string, 0, len(ports))
		for _, port := range ports {
			values = append(values, strconv.Itoa(int(port)))
		}
		match := fmt.Sprintf("%s %s dport { %s }", path, protocol, strings.Join(values, ", "))
		fmt.Fprintf(&lines, "add rule %s local_output %s ct direction original meta mark 0x%x meta mark set 0x%x queue num %d\n",
			table, match, plan.SocketMark(), plan.QueueMark(), plan.Queue())
		// Generated packets already carry the loop bit. Some retain the
		// input packet's mark; neither form may inherit a raw pass bit.
		for _, raw := range []uint32{plan.LoopMark(), plan.LoopMark() | plan.QueueMark()} {
			fmt.Fprintf(&lines, "add rule %s local_output %s meta mark 0x%x meta mark set 0x%x accept\n",
				table, match, raw, raw|plan.RawPassMark())
		}
	}
	fmt.Fprintf(&lines, "add rule %s local_output meta mark & 0x%x != 0 drop\n", table, plan.SocketMark()|plan.LoopMark())
	return lines.String(), nil
}
