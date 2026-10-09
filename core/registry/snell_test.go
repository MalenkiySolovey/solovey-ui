package registry

import "testing"

func TestSnellProductContractUsesOfficialCompatibility(t *testing.T) {
	in, out := SnellContract("inbounds"), SnellContract("outbounds")
	if in.Versions[0].Version != 5 || in.Versions[0].ClientVersion != 4 || out.Versions[0].Version != 4 || in.Versions[1].PSKMinBytes != 12 || in.URI {
		t.Fatal("incorrect pinned Snell compatibility")
	}
	for _, category := range []string{"inbounds", "outbounds"} {
		if fact := Resolve(category, "snell"); !fact.Available() || category == "inbounds" && !fact.AuthenticatedUsers {
			t.Fatal("Snell runtime not available or unsafe anonymous product capability")
		}
	}
	in.Versions[0].Version = 100
	if SnellContract("inbounds").Versions[0].Version != 5 {
		t.Fatal("caller mutated protocol facts")
	}
}
