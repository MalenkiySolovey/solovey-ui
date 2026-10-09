package capabilities

import (
	"net"
	"net/netip"
	"slices"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
)

type Interface struct {
	Name     string
	Eligible bool
}

type Resolve1NameState string

const (
	Resolve1NameUnknown        Resolve1NameState = "unknown"
	Resolve1NameUnclaimed      Resolve1NameState = "unclaimed"
	Resolve1NameCurrentProcess Resolve1NameState = "current-process"
	Resolve1NameOtherProcess   Resolve1NameState = "other-process"
)

// Environment is an observation, not a grant of network or D-Bus permission.
// The official runtime owner still authorizes/claims resources when starting.
// Deployment-specific tests/adapters can supply observations without OS guesses.
type Environment struct {
	InterfacesAvailable bool
	Interfaces          []Interface
	SystemBusAvailable  bool
	Resolve1Name        Resolve1NameState
}

func (e Environment) MulticastNames() []string {
	var names []string
	if e.InterfacesAvailable {
		for _, candidate := range e.Interfaces {
			if candidate.Eligible && candidate.Name != "" {
				names = append(names, candidate.Name)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func environmentEligibility(dependency string, e Environment) (bool, bool, string) {
	switch dependency {
	case "":
		return true, true, ""
	case registry.DependencyMulticast:
		if !e.InterfacesAvailable {
			return false, false, "DNS_INTERFACE_OBSERVATION_UNAVAILABLE"
		}
		if len(e.MulticastNames()) == 0 {
			return false, false, "DNS_MDNS_NO_USABLE_INTERFACE"
		}
		return true, true, ""
	case registry.DependencyResolve1:
		if !e.SystemBusAvailable {
			return false, false, "DNS_RESOLVED_SYSTEM_BUS_UNAVAILABLE"
		}
		switch e.Resolve1Name {
		case Resolve1NameUnclaimed, Resolve1NameCurrentProcess:
			return true, true, ""
		case Resolve1NameOtherProcess:
			return false, true, "DNS_RESOLVED_BUS_NAME_OCCUPIED"
		default:
			return false, true, "DNS_RESOLVED_BUS_NAME_UNKNOWN"
		}
	default:
		return false, false, "RUNTIME_DEPENDENCY_UNKNOWN"
	}
}

func optionalEligibility(value *bool, legacy bool) bool {
	if value == nil {
		return legacy
	}
	return *value
}

func ObserveEnvironment() Environment {
	var result Environment
	interfaces, err := net.Interfaces()
	if err == nil {
		result.InterfacesAvailable = true
		for _, candidate := range interfaces {
			eligible := candidate.Flags&net.FlagUp != 0 && candidate.Flags&net.FlagMulticast != 0 && candidate.Flags&net.FlagLoopback == 0
			if eligible {
				eligible = false
				addresses, addressErr := candidate.Addrs()
				if addressErr == nil {
					for _, address := range addresses {
						prefix, parseErr := netip.ParsePrefix(address.String())
						if parseErr == nil && !prefix.Addr().IsLoopback() && !prefix.Addr().Is4In6() {
							eligible = true
							break
						}
					}
				}
			}
			result.Interfaces = append(result.Interfaces, Interface{Name: candidate.Name, Eligible: eligible})
		}
	}
	result.SystemBusAvailable, result.Resolve1Name = observeResolve1()
	return result
}
