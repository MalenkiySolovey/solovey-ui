package firewall

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
)

func renderEndpointManagedNFTAt(plan FirewallPlan, managed bool, at time.Time) string {
	return nftables.RenderProtection(protectionIntent(plan), managed, at)
}
