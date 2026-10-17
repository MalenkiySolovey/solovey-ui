package resourceinventory

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	identity "github.com/MalenkiySolovey/solovey-ui/internal/networkidentity"
)

type interfaceObservation struct {
	intf   net.Interface
	addrs  []net.Addr
	failed bool
}

// Both read projections use this one observation seam. No sockets, routes,
// external services or platform commands are queried to infer a missing fact.
func (p *HostIngressScopeProviderV1) interfaceObservations(ctx context.Context) ([]interfaceObservation, error) {
	if p == nil || p.interfaces == nil || p.addrs == nil {
		return nil, errors.New("host_ingress_scope_provider_unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.New("host_interface_observation_cancelled")
	}
	interfaces, err := p.interfaces()
	if err != nil || len(interfaces) > hostresources.MaxResourceFacts {
		return nil, errors.New("host_ingress_scope_inventory_unavailable")
	}
	result := make([]interfaceObservation, 0, len(interfaces))
	addressCount := 0
	for _, intf := range interfaces {
		if ctx.Err() != nil {
			return nil, errors.New("host_interface_observation_cancelled")
		}
		observation := interfaceObservation{intf: intf, failed: true}
		if intf.Index > 0 && safeLocalInterfaceName(intf.Name) {
			observation.addrs, err = p.addrs(intf)
			observation.failed = err != nil
			addressCount += len(observation.addrs)
			if addressCount > hostresources.MaxResourceFacts {
				return nil, errors.New("host_interface_addresses_unbounded")
			}
		}
		result = append(result, observation)
	}
	if ctx.Err() != nil {
		return nil, errors.New("host_interface_observation_cancelled")
	}
	return result, nil
}

func safeLocalInterfaceName(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (p *HostIngressScopeProviderV1) networkNow() time.Time {
	if p != nil && p.now != nil {
		return p.now().UTC()
	}
	return time.Now().UTC()
}

func interfaceEvidence(value any, now time.Time) identity.Evidence {
	return identity.Evidence{Availability: identity.Available, Source: "host-network/net.Interfaces", Authority: "OS_INTERFACE_OBSERVATION",
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), Revision: identity.Revision(value), Reason: "local_interface_observation_not_network_role"}
}

// LocalHostNetworkIdentity is the single host-scoped read contract. Resource
// inventory supplies configuration inputs; they are never promoted to observed
// sockets, external reachability, a selected advertised policy or WAN/egress.
func (p *HostIngressScopeProviderV1) LocalHostNetworkIdentity(ctx context.Context, resources hostresources.ResourceSnapshot) identity.Snapshot {
	now := p.networkNow()
	result := identity.Empty(now)
	observations, err := p.interfaceObservations(ctx)
	if ctx.Err() != nil {
		result.InterfaceEvidence = identity.UnknownEvidence("interface_observation_cancelled")
		return identity.Finalize(result, p.networkNow())
	}
	if err == nil {
		complete := true
		seen := map[int]bool{}
		for _, observation := range observations {
			intf := observation.intf
			if intf.Index <= 0 || seen[intf.Index] || !safeLocalInterfaceName(intf.Name) {
				complete = false
				continue
			}
			seen[intf.Index] = true
			fact := identity.Interface{Index: intf.Index, Name: intf.Name, MTU: intf.MTU,
				Up: intf.Flags&net.FlagUp != 0, Loopback: intf.Flags&net.FlagLoopback != 0,
				VirtualNameHint: virtualInterfaceNameV1(intf.Name), Role: identity.UnknownEvidence("interface_role_not_observed"), Addresses: []identity.Address{}}
			fact.Evidence = interfaceEvidence(struct {
				Index, MTU int
				Name       string
				Flags      uint
			}{intf.Index, intf.MTU, intf.Name, uint(intf.Flags)}, now)
			addressComplete := !observation.failed
			for _, raw := range observation.addrs {
				if observation.failed || raw == nil {
					addressComplete = false
					continue
				}
				prefix, parseErr := netip.ParsePrefix(raw.String())
				if parseErr != nil {
					addressComplete = false
					continue
				}
				address, bits, family := prefix.Addr(), prefix.Bits(), "ipv6"
				if address.Is4In6() {
					address, bits = address.Unmap(), bits-96
					if bits < 0 {
						addressComplete = false
						continue
					}
				}
				if address.Is4() {
					family = "ipv4"
				}
				value := identity.Address{Address: address.String(), Prefix: bits, Family: family, Scope: identity.AddressScope(address)}
				value.Evidence = interfaceEvidence(struct {
					Index   int
					Address string
					Prefix  int
				}{intf.Index, value.Address, bits}, now)
				fact.Addresses = append(fact.Addresses, value)
			}
			sort.Slice(fact.Addresses, func(i, j int) bool {
				if fact.Addresses[i].Address != fact.Addresses[j].Address {
					return fact.Addresses[i].Address < fact.Addresses[j].Address
				}
				return fact.Addresses[i].Prefix < fact.Addresses[j].Prefix
			})
			fact.AddressEvidence = interfaceEvidence(fact.Addresses, now)
			if !addressComplete {
				fact.AddressEvidence.Availability, fact.AddressEvidence.Reason = identity.Unknown, "interface_addresses_incomplete"
				complete = false
			}
			for _, address := range fact.Addresses {
				if address.Scope == "private" {
					result.PrivateAddresses.Values = append(result.PrivateAddresses.Values, identity.AddressRef{InterfaceIndex: intf.Index,
						Address: address.Address, Prefix: address.Prefix, Evidence: address.Evidence})
				}
			}
			result.Interfaces = append(result.Interfaces, fact)
		}
		sort.Slice(result.Interfaces, func(i, j int) bool { return result.Interfaces[i].Index < result.Interfaces[j].Index })
		sort.Slice(result.PrivateAddresses.Values, func(i, j int) bool {
			a, b := result.PrivateAddresses.Values[i], result.PrivateAddresses.Values[j]
			if a.InterfaceIndex != b.InterfaceIndex {
				return a.InterfaceIndex < b.InterfaceIndex
			}
			return a.Address < b.Address
		})
		result.InterfaceEvidence = interfaceEvidence(result.Interfaces, now)
		result.PrivateAddresses.Evidence = interfaceEvidence(result.PrivateAddresses.Values, now)
		if !complete {
			result.InterfaceEvidence.Availability, result.InterfaceEvidence.Reason = identity.Unknown, "interface_inventory_incomplete"
			result.PrivateAddresses.Evidence.Availability, result.PrivateAddresses.Evidence.Reason = identity.Unknown, "interface_inventory_incomplete"
		}
	}
	projectConfiguredNetworkInputs(&result, resources, now)
	return identity.Finalize(result, p.networkNow())
}
