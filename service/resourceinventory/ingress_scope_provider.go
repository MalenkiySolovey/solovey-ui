package resourceinventory

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"sort"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const hostIngressScopeProviderIDV1 = "host-network"

// HostIngressScopeProviderV1 publishes exact interface observations but does
// not claim interception authority. An ordinary host interface inventory
// cannot prove that an interface is a provider-owned forwarded-ingress scope
// or that it is safe to exclude from management traffic.
type HostIngressScopeProviderV1 struct {
	now        func() time.Time
	interfaces func() ([]net.Interface, error)
	addrs      func(net.Interface) ([]net.Addr, error)
	goos       string
}

func NewHostIngressScopeProviderV1() *HostIngressScopeProviderV1 {
	return &HostIngressScopeProviderV1{
		now: time.Now, interfaces: net.Interfaces,
		addrs: func(value net.Interface) ([]net.Addr, error) { return value.Addrs() },
		goos:  runtime.GOOS,
	}
}

func (*HostIngressScopeProviderV1) ProviderID() string { return hostIngressScopeProviderIDV1 }

func (p *HostIngressScopeProviderV1) ForwardedIngressScopesV1(_ context.Context, now time.Time) ([]hostresources.ForwardedIngressScopeFactV1, error) {
	if p == nil || p.interfaces == nil || p.addrs == nil {
		return nil, errors.New("host_ingress_scope_provider_unavailable")
	}
	if now.IsZero() {
		now = time.Now()
		if p.now != nil {
			now = p.now()
		}
	}
	now = now.UTC()
	interfaces, err := p.interfaces()
	if err != nil || len(interfaces) > hostresources.MaxResourceFacts {
		return nil, errors.New("host_ingress_scope_inventory_unavailable")
	}
	result := make([]hostresources.ForwardedIngressScopeFactV1, 0, len(interfaces)*2)
	for _, intf := range interfaces {
		if intf.Index <= 0 || !safeInterfaceNameV1(intf.Name) {
			continue
		}
		addrs, addrErr := p.addrs(intf)
		if addrErr != nil {
			continue
		}
		families := map[hostresources.AddressFamily][]string{}
		for _, raw := range addrs {
			prefix, parseErr := netip.ParsePrefix(raw.String())
			if parseErr != nil {
				continue
			}
			family := hostresources.AddressFamilyIPv6
			if prefix.Addr().Unmap().Is4() {
				family = hostresources.AddressFamilyIPv4
			}
			families[family] = append(families[family], prefix.Masked().String())
		}
		for family, addresses := range families {
			sort.Strings(addresses)
			loopback := intf.Flags&net.FlagLoopback != 0
			virtual := virtualInterfaceNameV1(intf.Name)
			reasons := []string{"INGRESS_SCOPE_AUTHORITY_NOT_SHIPPED"}
			if p.goos != "linux" {
				reasons = append(reasons, "LINUX_PLATFORM_REQUIRED")
			}
			if intf.Flags&net.FlagUp == 0 {
				reasons = append(reasons, "INTERFACE_NOT_UP")
			}
			if loopback {
				reasons = append(reasons, "LOOPBACK_NOT_FORWARDED_INGRESS")
			}
			if virtual {
				reasons = append(reasons, "VIRTUAL_INTERFACE_PROVIDER_REQUIRED")
			}
			interfaceRevision := hostresources.Revision(struct {
				Schema, Name, MAC string
				Index             int
				Flags             uint
				Addresses         []string
			}{"solovey-ui/host-interface-observation/v1", intf.Name, intf.HardwareAddr.String(), intf.Index, uint(intf.Flags), addresses})
			ownership := hostresources.IngressScopeOwnershipUnknownV1
			external := false
			if virtual {
				ownership, external = hostresources.IngressScopeExternalManagedV1, true
			}
			fact, factErr := hostresources.FinalizeIngressScopeFactV1(hostresources.ForwardedIngressScopeFactV1{
				ProviderID: p.ProviderID(), ProviderRevision: hostresources.IngressScopeProviderRevisionV1,
				ScopeID: "host:interface:" + hostresources.Revision(struct {
					Index  int
					Family hostresources.AddressFamily
				}{intf.Index, family})[:32],
				InterfaceName: intf.Name, InterfaceIndex: intf.Index, InterfaceRevision: interfaceRevision,
				AddressFamily: family, Ownership: ownership, ForwardedIngress: false, Loopback: loopback,
				Virtual: virtual, Management: false, ExternalManaged: external,
				ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ReasonCodes: reasons,
			})
			if factErr == nil {
				result = append(result, fact)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ScopeID+"\x00"+string(result[i].AddressFamily) <
			result[j].ScopeID+"\x00"+string(result[j].AddressFamily)
	})
	return result, nil
}

func safeInterfaceNameV1(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func virtualInterfaceNameV1(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "kube", "tun", "tap", "wg", "tailscale"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

var _ hostresources.ForwardedIngressScopeProviderV1 = (*HostIngressScopeProviderV1)(nil)
