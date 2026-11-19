//go:build !minimal

package panelupdateui

import (
	_ "embed"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

//go:embed component.json
var componentJSON []byte

func init() {
	// Keep the previous input manifest byte-for-byte for the pinned Frontend.
	// The compatibility registration claims no HTTP or action authority.
	componentManifest := manifest.MustFromJSON(componentJSON)
	componentManifest.Actions = nil
	registry.Register(registry.Component{
		Manifest:  componentManifest,
		Lifecycle: lifecycle.Noop{},
	})
}
