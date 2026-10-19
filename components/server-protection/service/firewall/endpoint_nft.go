package firewall

import (
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"time"
)

func renderEndpointManagedNFTAt(plan FirewallPlan, managed bool, at time.Time) string {
	return nftables.RenderProtection(protectionIntent(plan), managed, at)
}
