package registry

import (
	C "github.com/sagernet/sing-box/constant"
	"testing"
)

func TestCapabilityRegistrationsAndCompiledImplementations(t *testing.T) {
	seen := map[string]bool{}
	for _, fact := range Facts() {
		key := fact.Category + ":" + fact.Type
		if seen[key] {
			t.Fatalf("duplicate capability %s", key)
		}
		seen[key] = true
		if !fact.Known || (fact.Compiled && !fact.Registered) {
			t.Fatalf("declaration disagrees with registry: %+v", fact)
		}
		if fact.Available() != (fact.Known && fact.Compiled && fact.Registered && fact.SupportedByProduct) {
			t.Fatalf("incorrect availability: %+v", fact)
		}
		if fact != Resolve(fact.Category, fact.Type) {
			t.Fatalf("resolve disagrees with projection: %+v", fact)
		}
	}
	for _, category := range []string{"inbounds", "outbounds"} {
		for _, protocol := range []string{"hysteria", "hysteria2", "tuic"} {
			fact := Resolve(category, protocol)
			if !fact.Registered || fact.Compiled != C.WithQUIC || fact.BuildTag != "with_quic" {
				t.Fatalf("QUIC schema/stub distinction: %+v", fact)
			}
		}
	}
	if fact := Resolve("outbounds", "naive"); fact.Compiled != SupportsNaiveOutbound || fact.Registered != SupportsNaiveOutbound {
		t.Fatalf("Naive registration: %+v", fact)
	}
	for _, pair := range [][2]string{{"endpoints", "tailscale"}, {"services", "derp"}, {"dns", "tailscale"}} {
		if fact := Resolve(pair[0], pair[1]); !fact.Registered || fact.Compiled != supportsTailscale {
			t.Fatalf("Tailscale stub: %+v", fact)
		}
	}
	if !Resolve("endpoints", "wireguard").Available() || !Resolve("services", "oom-killer").Available() {
		t.Fatal("direct official WireGuard/OOM implementations must remain available without include-aggregate flags")
	}
	if fact := Resolve("services", "resolved"); fact.Compiled != supportsResolved {
		t.Fatalf("official platform constructor: %+v", fact)
	}
	if fact := Resolve("endpoints", "unknown"); fact.Known || fact.Available() || fact.Reason != "UNKNOWN_ENTITY_TYPE" {
		t.Fatalf("unknown classification: %+v", fact)
	}
	facts := Facts()
	facts[0].Type = "changed"
	if Facts()[0].Type == "changed" {
		t.Fatal("caller mutated owner facts")
	}
}
