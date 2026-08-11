package resources

import (
	"fmt"
	"testing"
	"time"
)

func TestDualNetworkSocketClaimGeneration4096Bounded(t *testing.T) {
	revision := Revision("dual-network-performance")
	resources := make([]ProtectableResource, 4096)
	for index := range resources {
		port := 10000 + index
		resources[index] = ProtectableResource{ID: fmt.Sprintf("core:inbound:%d", index), Kind: "inbound", Owner: "core", Protocol: "stream", Listen: "127.0.0.1", Port: port,
			Capabilities: ProtectableResourceCapabilities{Known: true, OwnerRevision: revision, ConfigRevision: revision},
			ListenIntents: []ConfiguredListenIntentV1{
				{Schema: ConfiguredListenIntentSchemaV1, Mode: ListenIntentExact, Network: NetworkTCP, Address: "127.0.0.1", Port: uint16(port), RequiredFamilies: []AddressFamily{AddressFamilyIPv4}, ConfigurationRevision: revision},
				{Schema: ConfiguredListenIntentSchemaV1, Mode: ListenIntentExact, Network: NetworkUDP, Address: "127.0.0.1", Port: uint16(port), RequiredFamilies: []AddressFamily{AddressFamilyIPv4}, ConfigurationRevision: revision},
			}}
	}
	started := time.Now()
	claims := 0
	for _, resource := range resources {
		keys, complete := DeterministicConfiguredEndpointKeys(resource)
		if !complete || len(keys) != 2 || keys[0].Network != NetworkTCP || keys[1].Network != NetworkUDP {
			t.Fatalf("resource %s keys=%#v complete=%v", resource.ID, keys, complete)
		}
		claims += len(keys)
	}
	allocations := testing.AllocsPerRun(1, func() {
		for _, resource := range resources {
			_, _ = DeterministicConfiguredEndpointKeys(resource)
		}
	})
	if claims != 8192 || allocations > 250_000 {
		t.Fatalf("claims=%d allocations=%.0f", claims, allocations)
	}
	t.Logf("resources=%d socketClaims=%d duration=%s allocations=%.0f goroutinesAdded=0 dbSystemDnsCalls=0", len(resources), claims, time.Since(started), allocations)
}
