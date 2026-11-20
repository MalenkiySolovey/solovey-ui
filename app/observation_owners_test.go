//go:build !minimal

package app

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestObservationCompositionUsesSelectedOwners(t *testing.T) {
	empty := WithSelectedObservationOwners(componenthost.APIDeps{})
	if empty.ObservationSampler != nil || empty.ObservationHistory != nil || empty.AuditHistory != nil {
		t.Fatal("optional composition fell back to ambient Runtime")
	}
	for _, port := range []string{manifest.PortObservationHistory, manifest.PortAuditHistory} {
		if _, err := empty.BindAPI(manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{port}}); err == nil {
			t.Fatalf("missing %s was admitted", port)
		}
	}
	if _, err := (componenthost.Deps{}).BindLifecycle(manifest.HostAPI{Version: manifest.HostAPIV1, Lifecycle: []string{manifest.PortObservationSample}}); err == nil {
		t.Fatal("missing sampling owner was admitted")
	}
	configured := WithSelectedObservationOwners(componenthost.APIDeps{Runtime: service.NewRuntime(nil)})
	if _, selected := registry.ComponentByID("observability-extra"); !selected {
		if configured.ObservationSampler != nil || configured.ObservationHistory != nil || configured.AuditHistory != nil {
			t.Fatal("unselected observation owners were constructed")
		}
		return
	}
	if configured.ObservationSampler == nil || configured.ObservationHistory == nil || configured.AuditHistory == nil {
		t.Fatal("selected observation owners were not bound")
	}
	if _, err := configured.BindAPI(manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{manifest.PortAuditHistory}}); err == nil {
		t.Fatal("audit read admitted without audit authorization/rate boundaries")
	}
	ports, err := configured.BindAPI(manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{manifest.PortObservationHistory}})
	if err != nil || ports.ObservationHistory == nil || ports.ObservationSampler != nil || ports.AuditHistory != nil {
		t.Fatalf("history binding leaked undeclared owner ports: %+v %v", ports, err)
	}
}
