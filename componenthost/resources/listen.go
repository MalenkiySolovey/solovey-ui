package resources

import (
	"net/netip"
	"strings"
)

type ListenClass string

const (
	ListenWildcard     ListenClass = "wildcard"
	ListenIPv4Wildcard ListenClass = "ipv4_wildcard"
	ListenIPv6Wildcard ListenClass = "ipv6_wildcard"
	ListenLoopback     ListenClass = "loopback"
	ListenPublicExact  ListenClass = "public_exact"
	ListenHostname     ListenClass = "hostname"
)

type NormalizedListen struct {
	Value  string
	Class  ListenClass
	Family int
}

func (n NormalizedListen) Public() bool {
	return n.Class != ListenLoopback
}

func (n NormalizedListen) Wildcard() bool {
	return n.Class == ListenWildcard || n.Class == ListenIPv4Wildcard || n.Class == ListenIPv6Wildcard
}

func NormalizeListen(value string) NormalizedListen {
	value = strings.TrimSpace(value)
	if value == "" || value == "*" {
		return NormalizedListen{Value: "*", Class: ListenWildcard}
	}
	value = strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
	lower := strings.ToLower(strings.TrimSuffix(value, "."))
	switch lower {
	case "0.0.0.0":
		return NormalizedListen{Value: lower, Class: ListenIPv4Wildcard, Family: 4}
	case "::":
		return NormalizedListen{Value: lower, Class: ListenIPv6Wildcard, Family: 6}
	case "localhost":
		return NormalizedListen{Value: lower, Class: ListenLoopback}
	}
	if addr, err := netip.ParseAddr(lower); err == nil {
		addr = addr.Unmap()
		family := 6
		if addr.Is4() {
			family = 4
		}
		class := ListenPublicExact
		if addr.IsLoopback() {
			class = ListenLoopback
		}
		return NormalizedListen{Value: addr.String(), Class: class, Family: family}
	}
	return NormalizedListen{Value: lower, Class: ListenHostname}
}
