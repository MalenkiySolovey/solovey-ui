//go:build minimal

package componenthost

import (
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"testing"
)

func TestMinimalProfileDoesNotBindOptionalObservationPorts(t *testing.T) {
	d := (APIDeps{}).WithObservationOwners()
	for _, port := range []string{manifest.PortObservationHistory, manifest.PortAuditHistory} {
		if _, err := d.BindAPI(manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{port}}); err == nil {
			t.Fatalf("minimal profile admitted optional port %s", port)
		}
	}
	if _, err := (Deps{API: d}).BindLifecycle(manifest.HostAPI{Version: manifest.HostAPIV1, Lifecycle: []string{manifest.PortObservationSample}}); err == nil {
		t.Fatal("minimal profile admitted sampling")
	}
}
