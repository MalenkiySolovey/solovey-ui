package resourceinventory

import (
	"net"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestHostIngressScopeInventoryNeverInventsForwardingAuthority(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	provider := &HostIngressScopeProviderV1{
		now: func() time.Time { return now }, goos: "linux",
		interfaces: func() ([]net.Interface, error) {
			return []net.Interface{
				{Index: 2, Name: "eth0", Flags: net.FlagUp, HardwareAddr: net.HardwareAddr{0, 1, 2, 3, 4, 5}},
				{Index: 3, Name: "docker0", Flags: net.FlagUp, HardwareAddr: net.HardwareAddr{0, 1, 2, 3, 4, 6}},
				{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
			}, nil
		},
		addrs: func(value net.Interface) ([]net.Addr, error) {
			switch value.Name {
			case "eth0":
				return []net.Addr{&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)},
					&net.IPNet{IP: net.ParseIP("2001:db8::10"), Mask: net.CIDRMask(64, 128)}}, nil
			case "docker0":
				return []net.Addr{&net.IPNet{IP: net.ParseIP("172.17.0.1"), Mask: net.CIDRMask(16, 32)}}, nil
			default:
				return []net.Addr{&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)}}, nil
			}
		},
	}
	facts, err := provider.ForwardedIngressScopesV1(t.Context(), now)
	if err != nil {
		t.Fatalf("facts: %v", err)
	}
	if len(facts) != 4 {
		t.Fatalf("facts = %d, want 4: %#v", len(facts), facts)
	}
	seenFamilies := map[hostresources.AddressFamily]bool{}
	for _, fact := range facts {
		if fact.ForwardedIngress || fact.Ownership == hostresources.IngressScopeProviderManagedV1 {
			t.Fatalf("ordinary inventory invented authority: %#v", fact)
		}
		if fact.InterfaceName == "docker0" && (!fact.Virtual || !fact.ExternalManaged) {
			t.Fatalf("Docker interface was not an external-managed negative boundary: %#v", fact)
		}
		if fact.InterfaceName == "lo" && !fact.Loopback {
			t.Fatalf("loopback classification lost: %#v", fact)
		}
		if fact.InterfaceName == "eth0" {
			seenFamilies[fact.AddressFamily] = true
		}
	}
	if !seenFamilies[hostresources.AddressFamilyIPv4] || !seenFamilies[hostresources.AddressFamilyIPv6] {
		t.Fatalf("family-specific facts missing: %#v", seenFamilies)
	}
}

func TestHostIngressScopeRevisionChangesOnInterfaceRecreation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	index := 2
	provider := &HostIngressScopeProviderV1{
		now: func() time.Time { return now }, goos: "linux",
		interfaces: func() ([]net.Interface, error) {
			return []net.Interface{{Index: index, Name: "eth0", Flags: net.FlagUp}}, nil
		},
		addrs: func(net.Interface) ([]net.Addr, error) {
			return []net.Addr{&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)}}, nil
		},
	}
	first, _ := provider.ForwardedIngressScopesV1(t.Context(), now)
	index = 7
	second, _ := provider.ForwardedIngressScopesV1(t.Context(), now)
	if len(first) != 1 || len(second) != 1 || first[0].InterfaceRevision == second[0].InterfaceRevision ||
		first[0].ScopeID == second[0].ScopeID {
		t.Fatalf("interface recreation did not change exact identity: %#v %#v", first, second)
	}
}
