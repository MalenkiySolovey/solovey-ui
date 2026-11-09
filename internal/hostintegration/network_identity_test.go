package hostintegration

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"os"
	"testing"
	"time"

	identity "github.com/MalenkiySolovey/solovey-host/networkidentity"
)

func TestSnapshotCanonicalFixtures(t *testing.T) {
	data, err := os.ReadFile("../../host/fixtures/snapshots.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Schema string `json:"schema"`
		Cases  []struct {
			Name     string            `json:"name"`
			Now      int64             `json:"now"`
			Input    identity.Snapshot `json:"input"`
			Overflow string            `json:"overflow"`
			Expected json.RawMessage   `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Schema != "host-network-identity-fixtures/v1" || len(bundle.Cases) != 27 {
		t.Fatal("unexpected snapshot fixtures")
	}
	seen := map[string]bool{}
	for _, test := range bundle.Cases {
		t.Run(test.Name, func(t *testing.T) {
			if seen[test.Name] {
				t.Fatal("duplicate fixture")
			}
			seen[test.Name] = true
			value := test.Input
			switch test.Overflow {
			case "":
			case "interfaces":
				value.Interfaces = make([]identity.Interface, identity.MaxFacts+1)
			case "interface-addresses":
				value.Interfaces[0].Addresses = make([]identity.Address, identity.MaxFacts+1)
			case "listen":
				value.Listen.Values = make([]identity.EndpointInput, identity.MaxFacts+1)
			case "advertised":
				value.Advertised.Values = make([]identity.EndpointInput, identity.MaxFacts+1)
			case "private":
				value.PrivateAddresses.Values = make([]identity.AddressRef, identity.MaxFacts+1)
			case "lan":
				value.LAN.Values = make([]identity.AddressRef, identity.MaxFacts+1)
			case "wan":
				value.WAN.Values = make([]identity.AddressRef, identity.MaxFacts+1)
			case "routes":
				value.RouteSources.Values = make([]identity.AddressRef, identity.MaxFacts+1)
			case "egress":
				value.ExternalEgress.Values = make([]identity.AddressRef, identity.MaxFacts+1)
			default:
				t.Fatal("unknown fixture collection")
			}
			at := time.Unix(test.Now, 0)
			result := identity.Finalize(value, at)
			got, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var expected bytes.Buffer
			if err := json.Compact(&expected, test.Expected); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, expected.Bytes()) {
				t.Fatalf("canonical snapshot differs from accepted behavior:\ngot %s\nwant %s", got, expected.Bytes())
			}
			if identity.Finalize(result, at).Revision != result.Revision {
				t.Fatal("finalization changed the same observation revision")
			}
		})
	}
}

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
		if got := identity.AddressScope(netip.MustParseAddr(input)); got != want {
			t.Errorf("%s scope=%s, want %s", input, got, want)
		}
	}
	if identity.AddressScope(netip.Addr{}) != "unknown" {
		t.Fatal("invalid address became known")
	}
}
