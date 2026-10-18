package resources

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
)

func configuredIdentityFixture() ProtectableResource {
	return ProtectableResource{ID: "core:panel:web", Kind: "panel_web", Owner: "core", Source: "settings",
		Protocol: "http", Listen: "127.0.0.1", Port: 2095,
		Capabilities: ProtectableResourceCapabilities{ConfigRevision: Revision("panel configuration")}}
}

func TestResourceIdentityPreservesLegacyWireAndFingerprint(t *testing.T) {
	resource := configuredIdentityFixture()
	fingerprint := Fingerprint(resource)
	legacy, err := json.Marshal(resource)
	if err != nil || strings.Contains(string(legacy), `"identity"`) {
		t.Fatalf("legacy resource gained metadata: %s %v", legacy, err)
	}
	var decoded ProtectableResource
	if err := json.Unmarshal(legacy, &decoded); err != nil || decoded.Identity.ValidFor(decoded) {
		t.Fatal("missing legacy identity became known", err)
	}
	resource.Identity = LocalConfiguredIdentity(resource)
	if !resource.Identity.ValidFor(resource) || resource.Identity.Host != hostref.Local() ||
		resource.Identity.ObservationStatus != ResourceObservationUnknown || resource.Identity.ObservedRevision != "" {
		t.Fatalf("configured Local identity: %+v", resource.Identity)
	}
	payload, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	var oldReader struct{ ID, Owner, Source string }
	if err := json.Unmarshal(payload, &oldReader); err != nil || oldReader.ID != resource.ID || oldReader.Owner != resource.Owner || oldReader.Source != resource.Source {
		t.Fatalf("legacy identity changed: %+v %v", oldReader, err)
	}
	if err := json.Unmarshal(payload, &decoded); err != nil || !decoded.Identity.ValidFor(decoded) || Fingerprint(decoded) != fingerprint {
		t.Fatal("identity roundtrip changed the canonical fingerprint", err)
	}
}

func TestRegistryDropsUnboundIdentityWithoutChangingCanonicalOwnership(t *testing.T) {
	for name, change := range map[string]func(*ProtectableResource){
		"missing":                   func(r *ProtectableResource) { r.Identity = nil },
		"schema":                    func(r *ProtectableResource) { r.Identity.Schema = "future" },
		"remote":                    func(r *ProtectableResource) { r.Identity.Host = hostref.Ref{Kind: "REMOTE", ID: "node"} },
		"host absent":               func(r *ProtectableResource) { r.Identity.Host = hostref.Ref{} },
		"owner":                     func(r *ProtectableResource) { r.Identity.Owner = "foreign" },
		"resource alias":            func(r *ProtectableResource) { r.Identity.ResourceID = "core:panel:other" },
		"source":                    func(r *ProtectableResource) { r.Identity.Source = "other" },
		"stale desired":             func(r *ProtectableResource) { r.Capabilities.ConfigRevision = Revision("new configuration") },
		"claimed observed revision": func(r *ProtectableResource) { r.Identity.ObservedRevision = "observed" },
		"claimed readiness":         func(r *ProtectableResource) { r.Identity.ObservationStatus = "AVAILABLE" },
		"reason":                    func(r *ProtectableResource) { r.Identity.ReasonCode = "ready" },
	} {
		t.Run(name, func(t *testing.T) {
			resource := configuredIdentityFixture()
			resource.Identity = LocalConfiguredIdentity(resource)
			change(&resource)
			if resource.Identity.ValidFor(resource) {
				t.Fatal("unbound metadata validated")
			}
			registry := NewRegistry(time.Minute)
			registerTestContributor(t, registry, &testContributor{owner: "core", items: []ProtectableResource{resource}})
			got := registry.Refresh(t.Context())
			if len(got.Resources) != 1 || len(got.Errors) != 0 || len(got.Warnings) != 0 {
				t.Fatalf("optional metadata changed inventory admission: %+v", got)
			}
			item := got.Resources[0]
			if item.Identity != nil || item.ID != resource.ID || item.Owner != "core" || item.Capabilities.ConfigRevision != resource.Capabilities.ConfigRevision {
				t.Fatalf("metadata replaced canonical ownership: %+v", item)
			}
		})
	}
}

func TestResourceIdentityRequiresBoundedCanonicalInputs(t *testing.T) {
	for _, change := range []func(*ProtectableResource){
		func(r *ProtectableResource) { r.ID = "unsafe/id" },
		func(r *ProtectableResource) { r.Owner = " core" },
		func(r *ProtectableResource) { r.Source = "" },
		func(r *ProtectableResource) { r.Capabilities.ConfigRevision = "" },
		func(r *ProtectableResource) { r.Capabilities.ConfigRevision = strings.Repeat("r", 129) },
	} {
		resource := configuredIdentityFixture()
		change(&resource)
		if LocalConfiguredIdentity(resource) != nil {
			t.Fatal("invalid canonical input gained identity")
		}
	}
}

func TestRegistryIdentityIsClonedAndHasOneCanonicalOwner(t *testing.T) {
	resource := configuredIdentityFixture()
	resource.Identity = LocalConfiguredIdentity(resource)
	registry := NewRegistry(time.Minute)
	registerTestContributor(t, registry, &testContributor{owner: "core", items: []ProtectableResource{resource}})
	first := registry.Refresh(t.Context())
	first.Resources[0].Identity.Owner = "caller mutation"
	if resource.Identity.Owner != "core" || !registry.Snapshot(t.Context()).Resources[0].Identity.ValidFor(resource) {
		t.Fatal("snapshot caller mutated a contributor or cached identity")
	}
	resource.Identity.Owner = "contributor mutation"
	if !registry.Snapshot(t.Context()).Resources[0].Identity.ValidFor(configuredIdentityFixture()) {
		t.Fatal("contributor mutated the cached identity")
	}
	foreign := configuredIdentityFixture()
	foreign.Owner = "foreign"
	foreign.Identity = LocalConfiguredIdentity(foreign)
	registerTestContributor(t, registry, &testContributor{owner: "foreign", items: []ProtectableResource{foreign}})
	resource.Identity.Owner = "core"
	current := registry.Refresh(t.Context())
	if len(current.Resources) != 1 || current.Resources[0].Owner != "core" || !current.Resources[0].Identity.ValidFor(current.Resources[0]) || len(current.Warnings) != 1 || current.Warnings[0].Code != "duplicate_resource_id" {
		t.Fatalf("duplicate metadata created a second owner: %+v", current)
	}
}
