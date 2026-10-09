package capabilities

import (
	"reflect"
	"runtime"
	"testing"
)

func TestDNSObservationSeparatesBuildAndRuntime(t *testing.T) {
	for _, test := range []struct {
		name        string
		environment Environment
		mdns        bool
		resolved    bool
		dependency  bool
	}{
		{"unobserved", Environment{}, false, false, false},
		{"no multicast", Environment{InterfacesAvailable: true}, false, false, false},
		{"free bus", Environment{SystemBusAvailable: true, Resolve1Name: Resolve1NameUnclaimed}, false, true, true},
		{"own bus", Environment{SystemBusAvailable: true, Resolve1Name: Resolve1NameCurrentProcess}, false, true, true},
		{"occupied bus", Environment{SystemBusAvailable: true, Resolve1Name: Resolve1NameOtherProcess}, false, false, true},
		{"unknown owner", Environment{SystemBusAvailable: true}, false, false, true},
		{"multicast", Environment{InterfacesAvailable: true, Interfaces: []Interface{{Name: "fixture", Eligible: true}}}, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			mdns := ResolveWithEnvironment("dns", "mdns", test.environment)
			if !mdns.Known || !mdns.Registered || !mdns.Compiled || mdns.Available != test.mdns {
				t.Fatalf("mDNS build/runtime distinction: %+v", mdns)
			}
			for _, category := range []string{"dns", "services"} {
				resolved := ResolveWithEnvironment(category, "resolved", test.environment)
				if !resolved.Known || resolved.Available != (runtime.GOOS == "linux" && test.resolved) || *resolved.PlatformDependencyAvailable != test.dependency {
					t.Fatalf("resolved build/runtime distinction: %+v", resolved)
				}
			}
			if !ResolveWithEnvironment("dns", "local", test.environment).Available {
				t.Fatal("environment constrained unrelated DNS")
			}
		})
	}
}

func TestCapturedDNSObservationIsRequiredAndIndependent(t *testing.T) {
	environment := Environment{InterfacesAvailable: true, Interfaces: []Interface{{Name: "b", Eligible: true}, {Name: "a", Eligible: true}, {Name: "a", Eligible: true}, {Name: "loopback"}}}
	snapshot := CurrentWithEnvironment(environment)
	if !reflect.DeepEqual(snapshot.MulticastInterfaces, []string{"a", "b"}) {
		t.Fatal("interface projection is not deterministic")
	}
	fact := snapshot.Resolve("dns", "mdns")
	*fact.RuntimeEligible = false
	if !snapshot.Resolve("dns", "mdns").Available {
		t.Fatal("resolved copy mutated captured snapshot")
	}
	for i := range snapshot.Facts {
		if snapshot.Facts[i].Category == "dns" && snapshot.Facts[i].Type == "mdns" {
			snapshot.Facts[i].RuntimeEligible = nil
		}
	}
	if snapshot.Resolve("dns", "mdns").Available {
		t.Fatal("missing dependency capture authorized multicast")
	}
}
