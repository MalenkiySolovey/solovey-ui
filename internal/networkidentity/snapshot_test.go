package networkidentity

import (
	"net/netip"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
)

func TestAddressRangeFactsDoNotClaimLANWANOrReachability(t *testing.T) {
	for input, want := range map[string]string{
		"0.0.0.0": "unspecified", "0.0.0.1": "other_special", "10.1.2.3": "private", "100.64.0.1": "shared_cgnat",
		"100.127.255.254": "shared_cgnat", "127.0.0.1": "loopback", "169.254.1.2": "link_local", "172.16.2.1": "private",
		"192.0.0.1": "other_special", "192.0.2.1": "documentation", "192.168.1.1": "private", "198.18.1.1": "benchmark",
		"198.51.100.1": "documentation", "203.0.113.1": "documentation", "224.1.1.1": "multicast", "240.1.1.1": "other_special",
		"255.255.255.255": "other_special", "8.8.8.8": "global_unicast", "::": "unspecified", "::1": "loopback",
		"fe80::1": "link_local", "fd00::1": "private", "ff0e::1": "multicast", "2001:db8::1": "documentation",
		"2001:4860::1": "global_unicast", "::ffff:100.64.1.2": "shared_cgnat", "224.0.0.1": "multicast", "ff02::1": "multicast",
	} {
		if got := AddressScope(netip.MustParseAddr(input)); got != want {
			t.Errorf("%s scope=%s, want %s", input, got, want)
		}
	}
	if AddressScope(netip.Addr{}) != "unknown" {
		t.Fatal("invalid address became known")
	}
}

func TestSnapshotRejectsUnboundedProviderProjection(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	value := Empty(now)
	value.Interfaces = make([]Interface, MaxFacts+1)
	if result := Finalize(value, now); len(result.Interfaces) != 0 || result.InterfaceEvidence.Availability != Unknown {
		t.Fatal("unbounded interface projection escaped")
	}
	value = Empty(now)
	value.Listen.Values = make([]EndpointInput, MaxFacts+1)
	if result := Finalize(value, now); len(result.Listen.Values) != 0 || result.Listen.Evidence.Availability != Unknown {
		t.Fatal("unbounded endpoint projection escaped")
	}
}

func TestSnapshotAgesEveryValueAndPreservesLocalBinding(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	e := Evidence{Availability: Available, Source: "host-network/net.Interfaces", Authority: "OS_INTERFACE_OBSERVATION",
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), Revision: Revision("fixture"), Reason: "observed"}
	value := Empty(now)
	value.InterfaceEvidence, value.PrivateAddresses.Evidence = e, e
	value.Interfaces = []Interface{{Index: 2, Name: "eth0", Evidence: e, AddressEvidence: e,
		Role: UnknownEvidence("no_role_provider"), Addresses: []Address{{Address: "192.168.1.2", Prefix: 24, Evidence: e}}}}
	value.PrivateAddresses.Values = []AddressRef{{InterfaceIndex: 2, Address: "192.168.1.2", Evidence: e}}
	value.Listen = EndpointFacts{Evidence: e, Values: []EndpointInput{{Address: "0.0.0.0", Port: 443, Evidence: e}}}
	fresh := Finalize(value, now)
	stale := Finalize(fresh, now.Add(time.Minute))
	if !fresh.Host.IsLocal() || fresh.InterfaceEvidence.Availability != Available || stale.InterfaceEvidence.Availability != Unknown ||
		stale.Interfaces[0].Evidence.Availability != Unknown || stale.Interfaces[0].Addresses[0].Evidence.Availability != Unknown ||
		stale.PrivateAddresses.Values[0].Evidence.Availability != Unknown || stale.Listen.Values[0].Evidence.Availability != Unknown {
		t.Fatalf("fresh=%+v stale=%+v", fresh, stale)
	}
	stale.Interfaces[0].Addresses[0].Address = "changed"
	stale.PrivateAddresses.Values[0].Address = "changed"
	stale.Listen.Values[0].Address = "changed"
	if fresh.Interfaces[0].Addresses[0].Address != "192.168.1.2" || fresh.PrivateAddresses.Values[0].Address != "192.168.1.2" || fresh.Listen.Values[0].Address != "0.0.0.0" {
		t.Fatal("consumer projection aliases prior snapshot")
	}
	if Finalize(value, now).Revision != fresh.Revision {
		t.Fatal("same observation produced a different revision")
	}
	value.Host = hostref.Ref{Kind: "NODE", ID: "other"}
	if got := Finalize(value, now); len(got.Interfaces) != 0 || !got.Host.IsLocal() || got.InterfaceEvidence.Availability != Unknown {
		t.Fatal("unshipped remote host became Local authority")
	}
}

func TestUnknownEvidenceCannotBecomeAvailable(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	base := Evidence{Availability: Available, Source: "host-network/net.Interfaces", Authority: "OS_INTERFACE_OBSERVATION",
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), Revision: Revision("fixture")}
	for name, change := range map[string]func(*Evidence){
		"unknown provider":     func(e *Evidence) { e.Source = "unknown-provider" },
		"unknown authority":    func(e *Evidence) { e.Authority = "" },
		"bad revision":         func(e *Evidence) { e.Revision = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" },
		"future observation":   func(e *Evidence) { e.ObservedAt++ },
		"unbounded freshness":  func(e *Evidence) { e.ExpiresAt = e.ObservedAt + 301 },
		"unknown availability": func(e *Evidence) { e.Availability = "FUTURE_VALUE" },
	} {
		t.Run(name, func(t *testing.T) {
			e := base
			change(&e)
			if e.At(now).Availability != Unknown {
				t.Fatalf("invalid evidence became current: %+v", e)
			}
		})
	}
	if UnknownEvidence("not_shipped").At(now).Availability != Unknown {
		t.Fatal("absence became positive evidence")
	}
}
