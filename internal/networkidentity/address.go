package networkidentity

import "net/netip"

// These are conservative address-range facts, never LAN/WAN roles or proof of
// public reachability. In particular shared CGNAT space is not public egress.
var specialScopes = []struct {
	prefix netip.Prefix
	scope  string
}{
	{netip.MustParsePrefix("100.64.0.0/10"), "shared_cgnat"},
	{netip.MustParsePrefix("192.0.2.0/24"), "documentation"},
	{netip.MustParsePrefix("198.51.100.0/24"), "documentation"},
	{netip.MustParsePrefix("203.0.113.0/24"), "documentation"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentation"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmark"},
	{netip.MustParsePrefix("0.0.0.0/8"), "other_special"},
	{netip.MustParsePrefix("192.0.0.0/24"), "other_special"},
	{netip.MustParsePrefix("240.0.0.0/4"), "other_special"},
}

func AddressScope(address netip.Addr) string {
	address = address.Unmap()
	switch {
	case !address.IsValid():
		return "unknown"
	case address.IsUnspecified():
		return "unspecified"
	case address.IsLoopback():
		return "loopback"
	case address.IsLinkLocalUnicast():
		return "link_local"
	case address.IsPrivate():
		return "private"
	case address.IsMulticast():
		return "multicast"
	}
	for _, special := range specialScopes {
		if special.prefix.Contains(address) {
			return special.scope
		}
	}
	if address.IsGlobalUnicast() {
		return "global_unicast"
	}
	return "other_special"
}
