package resourceinventory

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestConfiguredListenerIdentityUsesExistingCoreOwnerAndRevisions(t *testing.T) {
	settings := fakeSettings{listen: "127.0.0.1", port: 2095, subListen: "::1", subPort: 2096}
	contributors := []hostresources.ResourceContributor{panelContributor{settings}, subscriptionContributor{settings}}
	for index, contributor := range contributors {
		items, err := contributor.ListProtectableResources(t.Context())
		if err != nil || len(items) != 1 {
			t.Fatal(items, err)
		}
		resource := items[0]
		wantID := []string{"core:panel:web", "core:subscription:default"}[index]
		if resource.ID != wantID || resource.Owner != contributor.Owner() || resource.Owner != "core" ||
			!resource.Identity.ValidFor(resource) || resource.Identity.Host != hostref.Local() ||
			resource.Identity.DesiredRevision != resource.Capabilities.OwnerRevision ||
			resource.Identity.ObservationStatus != hostresources.ResourceObservationUnknown || resource.Identity.ObservedRevision != "" {
			t.Fatalf("listener identity: %+v", resource)
		}
		payload, err := json.Marshal(resource)
		if err != nil {
			t.Fatal(err)
		}
		var decoded hostresources.ProtectableResource
		if err := json.Unmarshal(payload, &decoded); err != nil || decoded.ID != wantID || !decoded.Identity.ValidFor(decoded) {
			t.Fatal("listener ID roundtrip changed", err)
		}
		legacy := resource
		legacy.Identity = nil
		if hostresources.Fingerprint(resource) != hostresources.Fingerprint(legacy) {
			t.Fatal("metadata changed the legacy fingerprint")
		}
	}
}

func TestConfiguredListenerIdentityRegeneratesFromDesiredSettings(t *testing.T) {
	first := listenerFromSettings(listenerSettings{id: "core:panel:web", kind: "panel_web", listen: "127.0.0.1", port: 2095})
	second := listenerFromSettings(listenerSettings{id: first.ID, kind: first.Kind, listen: first.Listen, port: 2096})
	if !second.Identity.ValidFor(second) || first.Identity.ValidFor(second) || first.ID != second.ID || first.Owner != second.Owner || first.Identity.DesiredRevision == second.Identity.DesiredRevision {
		t.Fatal("changed configuration reused stale metadata or changed identity")
	}
}
