package firewall

import (
	"time"
)

func renderEndpointManagedNFTAt(plan FirewallPlan, managed bool, at time.Time, codec NFTCodec) string {
	if codec == nil {
		return ""
	}
	return codec.RenderProtection(protectionIntent(plan), managed, at)
}
