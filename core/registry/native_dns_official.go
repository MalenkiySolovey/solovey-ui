//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/dns"

const supportsNativeDNS = false

func registerSDNS(*dns.TransportRegistry)        {}
func registerFallbackDNS(*dns.TransportRegistry) {}
