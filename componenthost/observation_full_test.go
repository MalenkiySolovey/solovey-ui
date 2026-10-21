//go:build !minimal

package componenthost

import (
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"testing"
)

func TestObservationCompositionRequiresExplicitRuntimeAndBindingRequiresOwners(t *testing.T) {
	empty := (APIDeps{}).WithObservationOwners()
	if empty.ObservationSampler != nil || empty.ObservationHistory != nil || empty.AuditHistory != nil {
		t.Fatal("optional composition fell back to ambient Runtime")
	}
	configured := (APIDeps{Runtime: service.NewRuntime(nil)}).WithObservationOwners()
	if configured.ObservationSampler == nil || configured.ObservationHistory == nil || configured.AuditHistory == nil {
		t.Fatal("core composition did not inject existing owners")
	}
	for _, port := range []string{manifest.PortObservationHistory, manifest.PortAuditHistory} {
		if _, err := empty.BindAPI(manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{port}}); err == nil {
			t.Fatalf("missing %s was admitted", port)
		}
	}
	if _, err := (Deps{}).BindLifecycle(manifest.HostAPI{Version: manifest.HostAPIV1, Lifecycle: []string{manifest.PortObservationSample}}); err == nil {
		t.Fatal("missing sampling owner was admitted")
	}
	if _, err := configured.BindAPI(manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{manifest.PortAuditHistory}}); err == nil {
		t.Fatal("audit read admitted without audit authorization/rate boundaries")
	}
	ports, err := configured.BindAPI(manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{manifest.PortObservationHistory}})
	if err != nil || ports.ObservationHistory == nil || ports.ObservationSampler != nil || ports.AuditHistory != nil {
		t.Fatalf("history binding leaked undeclared owner ports: %+v %v", ports, err)
	}
}
