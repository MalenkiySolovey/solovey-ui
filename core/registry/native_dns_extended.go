//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/dns/transport/fallback"
)

const supportsNativeDNS = true

func registerSDNS(r *dns.TransportRegistry)        { transport.RegisterSDNS(r) }
func registerFallbackDNS(r *dns.TransportRegistry) { fallback.RegisterTransport(r) }
