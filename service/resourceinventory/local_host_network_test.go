package resourceinventory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	identity "github.com/MalenkiySolovey/solovey-ui/internal/networkidentity"
)

type networkFixtureAddress string

func (networkFixtureAddress) Network() string  { return "fixture" }
func (a networkFixtureAddress) String() string { return string(a) }

func networkFixture(now time.Time, interfaces []net.Interface, addresses map[int][]string) *HostIngressScopeProviderV1 {
	return &HostIngressScopeProviderV1{now: func() time.Time { return now }, goos: "linux",
		interfaces: func() ([]net.Interface, error) { return interfaces, nil },
		addrs: func(value net.Interface) ([]net.Addr, error) {
			result := []net.Addr{}
			for _, address := range addresses[value.Index] {
				result = append(result, networkFixtureAddress(address))
			}
			return result, nil
		}}
}

func requireUnsupportedNetworkFacts(t *testing.T, snapshot identity.Snapshot) {
	t.Helper()
	for name, facts := range map[string]identity.AddressFacts{"LAN": snapshot.LAN, "WAN": snapshot.WAN, "route": snapshot.RouteSources, "egress": snapshot.ExternalEgress} {
		if facts.Evidence.Availability != identity.Unknown || facts.Evidence.Reason == "" || len(facts.Values) != 0 {
			t.Fatalf("unsupported %s became authority: %+v", name, facts)
		}
	}
	if snapshot.ForwardedIngress.Availability != identity.Unknown || snapshot.ForwardedIngress.Reason != "INGRESS_SCOPE_AUTHORITY_NOT_SHIPPED" {
		t.Fatalf("forwarding authority=%+v", snapshot.ForwardedIngress)
	}
}

func TestLocalHostNetworkKeepsAddressFactsDistinct(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, test := range []struct {
		name      string
		addresses []string
		private   int
	}{
		{"single IPv4", []string{"8.8.8.8/32"}, 0},
		{"single IPv6", []string{"2001:4860::1/64"}, 0},
		{"dual stack", []string{"192.168.50.7/24", "fd00::7/64"}, 2},
		{"private public shared", []string{"10.1.2.3/24", "8.8.8.8/32", "100.64.1.2/24"}, 1},
		{"loopback link local", []string{"127.0.0.1/8", "::1/128", "169.254.1.1/16", "fe80::1/64"}, 0},
		{"mapped IPv4", []string{"::ffff:192.168.1.2/120"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := networkFixture(now, []net.Interface{{Index: 2, Name: "wan", Flags: net.FlagUp}}, map[int][]string{2: test.addresses})
			calls := 0
			observe := provider.interfaces
			provider.interfaces = func() ([]net.Interface, error) { calls++; return observe() }
			facts := provider.LocalHostNetworkIdentity(t.Context(), hostresources.ResourceSnapshot{})
			if calls != 1 || facts.InterfaceEvidence.Availability != identity.Available || len(facts.Interfaces) != 1 ||
				len(facts.Interfaces[0].Addresses) != len(test.addresses) || len(facts.PrivateAddresses.Values) != test.private {
				t.Fatalf("calls=%d facts=%+v", calls, facts)
			}
			if facts.Interfaces[0].Role.Availability != identity.Unknown || !facts.Host.IsLocal() {
				t.Fatal("interface name supplied a role/host authority")
			}
			if test.name == "dual stack" && facts.Interfaces[0].Addresses[0].Address != "192.168.50.7" {
				t.Fatal("interface address was replaced by its masked network prefix")
			}
			requireUnsupportedNetworkFacts(t, facts)
		})
	}
}

func TestLocalHostNetworkMultipleDownVirtualAndWindowsNames(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	provider := networkFixture(now, []net.Interface{
		{Index: 3, Name: "docker0", Flags: net.FlagUp}, {Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Index: 2, Name: "Ethernet 2 — сеть"},
	}, map[int][]string{3: {"172.17.0.1/16"}, 1: {"127.0.0.1/8"}, 2: {"192.168.1.2/24"}})
	provider.goos = "windows"
	facts := provider.LocalHostNetworkIdentity(t.Context(), hostresources.ResourceSnapshot{})
	if len(facts.Interfaces) != 3 || facts.Interfaces[0].Index != 1 || !facts.Interfaces[0].Loopback ||
		facts.Interfaces[1].Up || facts.Interfaces[1].Name != "Ethernet 2 — сеть" || !facts.Interfaces[2].VirtualNameHint {
		t.Fatalf("interface observations lost: %+v", facts.Interfaces)
	}
	requireUnsupportedNetworkFacts(t, facts)
	// The legacy interception contract keeps its stricter interface-name rules.
	legacy, err := provider.ForwardedIngressScopesV1(t.Context(), now)
	if err != nil || len(legacy) != 2 {
		t.Fatalf("legacy facts=%+v error=%v", legacy, err)
	}
}

func TestLocalHostNetworkUnknownPartialBoundsAndCancellation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, test := range []struct {
		name   string
		change func(*HostIngressScopeProviderV1)
	}{
		{"provider absent", func(p *HostIngressScopeProviderV1) { p.interfaces = nil }},
		{"provider failure", func(p *HostIngressScopeProviderV1) {
			p.interfaces = func() ([]net.Interface, error) { return nil, errors.New("private-host-detail") }
		}},
		{"addresses unavailable", func(p *HostIngressScopeProviderV1) {
			p.addrs = func(net.Interface) ([]net.Addr, error) { return nil, errors.New("private-address-detail") }
		}},
		{"malformed address", func(p *HostIngressScopeProviderV1) {
			p.addrs = func(net.Interface) ([]net.Addr, error) { return []net.Addr{networkFixtureAddress("invalid")}, nil }
		}},
		{"too many interfaces", func(p *HostIngressScopeProviderV1) {
			p.interfaces = func() ([]net.Interface, error) { return make([]net.Interface, hostresources.MaxResourceFacts+1), nil }
		}},
		{"too many addresses", func(p *HostIngressScopeProviderV1) {
			p.addrs = func(net.Interface) ([]net.Addr, error) {
				return make([]net.Addr, hostresources.MaxResourceFacts+1), nil
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := networkFixture(now, []net.Interface{{Index: 2, Name: "eth0"}}, nil)
			test.change(provider)
			facts := provider.LocalHostNetworkIdentity(t.Context(), hostresources.ResourceSnapshot{})
			payload, _ := json.Marshal(facts)
			if facts.InterfaceEvidence.Availability != identity.Unknown || bytes.Contains(payload, []byte("private-host-detail")) || bytes.Contains(payload, []byte("private-address-detail")) {
				t.Fatalf("unknown provider facts=%s", payload)
			}
			requireUnsupportedNetworkFacts(t, facts)
		})
	}
	provider := networkFixture(now, nil, nil)
	provider.interfaces = func() ([]net.Interface, error) { t.Fatal("cancelled read observed interfaces"); return nil, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if facts := provider.LocalHostNetworkIdentity(ctx, hostresources.ResourceSnapshot{}); facts.InterfaceEvidence.Availability != identity.Unknown {
		t.Fatal("cancelled read became known")
	}
	if facts := (*HostIngressScopeProviderV1)(nil).LocalHostNetworkIdentity(t.Context(), hostresources.ResourceSnapshot{}); facts.InterfaceEvidence.Availability != identity.Unknown {
		t.Fatal("nil provider became known")
	}
}

func TestLocalHostNetworkConfiguredInputsRemainInputs(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	provider := networkFixture(now, nil, nil)
	resource := listenerFromSettings(listenerSettings{id: "core:panel:web", kind: "panel_web", listen: "192.168.1.2", port: 443, domain: "panel.example", fallback: hostresources.CapabilityYes})
	resource.AdvertisedEndpoints = []hostresources.AdvertisedEndpoint{{ID: "explicit", HostnameOrIP: "vpn.example", Port: 8443, Network: hostresources.NetworkTCP}}
	resources := hostresources.ResourceSnapshot{GeneratedAt: now.Unix(), Resources: []hostresources.ProtectableResource{resource}}
	before, _ := json.Marshal(resources)
	facts := provider.LocalHostNetworkIdentity(t.Context(), resources)
	if facts.Listen.Evidence.Availability != identity.Available || len(facts.Listen.Values) != 1 || facts.Listen.Values[0].Address != "192.168.1.2" ||
		facts.Advertised.Evidence.Availability != identity.Available || len(facts.Advertised.Values) != 2 {
		t.Fatalf("configured inputs=%+v %+v", facts.Listen, facts.Advertised)
	}
	for _, value := range append(facts.Listen.Values, facts.Advertised.Values...) {
		if value.Evidence.Authority != "CONFIGURED_INPUT" || value.ConfigurationRevision != resource.Capabilities.ConfigRevision {
			t.Fatal("configuration promoted to observed authority")
		}
	}
	stale := identity.Finalize(facts, now.Add(5*time.Second))
	if stale.Listen.Evidence.Availability != identity.Unknown || stale.Listen.Values[0].Evidence.Availability != identity.Unknown {
		t.Fatal("stale configured read remained current")
	}
	resources.GeneratedAt -= 10
	if old := provider.LocalHostNetworkIdentity(t.Context(), resources); old.Listen.Evidence.Availability != identity.Unknown || len(old.Listen.Values) != 0 {
		t.Fatal("stale resource inventory was re-observed as new")
	}
	resources.GeneratedAt = now.Unix()
	resources.Errors = []hostresources.ResourceError{{Owner: "other", Message: "private-error"}}
	if partial := provider.LocalHostNetworkIdentity(t.Context(), resources); partial.Listen.Evidence.Availability != identity.Unknown || len(partial.Listen.Values) != 1 {
		t.Fatal("incomplete inventory lost partial truth or claimed completeness")
	}
	resources.Errors = nil
	after, _ := json.Marshal(resources)
	if !bytes.Equal(before, after) || !reflect.DeepEqual(resource.AdvertisedEndpoints, resources.Resources[0].AdvertisedEndpoints) {
		t.Fatal("read changed explicit resource configuration")
	}
	requireUnsupportedNetworkFacts(t, facts)
}

func TestLocalHostNetworkActualInterfaceRead(t *testing.T) {
	facts := NewHostIngressScopeProviderV1().LocalHostNetworkIdentity(t.Context(), hostresources.ResourceSnapshot{})
	if !facts.Host.IsLocal() || facts.Schema != identity.SchemaV1 || facts.Revision == "" ||
		facts.InterfaceEvidence.Availability != identity.Available || len(facts.Interfaces) == 0 || facts.ObservationScope != "CURRENT_PROCESS_NETWORK_NAMESPACE" {
		t.Fatalf("actual interface read: availability=%s reason=%s count=%d", facts.InterfaceEvidence.Availability, facts.InterfaceEvidence.Reason, len(facts.Interfaces))
	}
	for _, intf := range facts.Interfaces {
		if intf.Index <= 0 || intf.Name == "" || intf.Role.Availability != identity.Unknown {
			t.Fatal("actual interface fact has no identity or acquired an inferred role")
		}
	}
	requireUnsupportedNetworkFacts(t, facts)
}

func TestLocalHostNetworkKeepsLegacyIngressIdentity(t *testing.T) {
	// Frozen pre-F3 hash inputs at main 85957e60: family-specific masked
	// address list, interface name/index/MAC/flags, and unchanged scope key.
	now := time.Unix(1_800_000_000, 0)
	provider := networkFixture(now, []net.Interface{{Index: 2, Name: "eth0", Flags: net.FlagUp}}, map[int][]string{2: {"192.168.1.2/24"}})
	facts, err := provider.ForwardedIngressScopesV1(t.Context(), now)
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts=%d error=%v", len(facts), err)
	}
	fact := facts[0]
	if fact.InterfaceRevision != "7066bb80fec5b241e8f64ca767851992bd6b261e282c4f13446d6d67c558304f" ||
		fact.ScopeID != "host:interface:66d3def36b9a39cd407f07454a1e26c5" || fact.ForwardedIngress ||
		fact.ObservedAt != now.Unix() || fact.ExpiresAt != now.Add(time.Minute).Unix() {
		t.Fatalf("legacy ingress identity changed: %+v", fact)
	}
}
