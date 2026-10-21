package registry

import (
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"testing"
)

func TestComponentActionAdmissionAndImmutableDeclarations(t *testing.T) {
	base := Component{Manifest: manifest.Manifest{ID: "panel-update-ui", Name: "Panel Update UI", Version: "1", Delivery: manifest.DeliveryInProcess, Actions: []string{actions.ComponentRemove}}, Lifecycle: lifecycle.Noop{}}
	for _, variant := range []string{"unknown", "foreign", "core_action", "duplicate"} {
		t.Run(variant, func(t *testing.T) {
			component := base
			component.Manifest.Actions = append([]string(nil), base.Manifest.Actions...)
			switch variant {
			case "unknown":
				component.Manifest.Actions[0] = "future.execute"
			case "foreign":
				component.Manifest.ID = "telegram"
			case "core_action":
				component.Manifest.Actions[0] = actions.BackupRestore
			case "duplicate":
				component.Manifest.Actions = append(component.Manifest.Actions, actions.ComponentRemove)
			}
			registry := newRegistry()
			admitted := false
			func() {
				defer func() {
					if recover() == nil {
						t.Error("invalid component action admitted")
					}
				}()
				registry.registerWith(component, func(manifest.Manifest) { admitted = true })
			}()
			if admitted || len(registry.components) != 0 {
				t.Fatal("invalid declaration changed owner admission")
			}
		})
	}
	registry := newRegistry()
	registry.register(base)
	base.Manifest.Actions[0] = "future.execute"
	snapshot, _ := registry.componentByID("panel-update-ui")
	snapshot.Manifest.Actions[0] = "future.execute"
	stored, _ := registry.componentByID("panel-update-ui")
	if len(stored.Manifest.Actions) != 1 || stored.Manifest.Actions[0] != actions.ComponentRemove {
		t.Fatal("caller mutated registered action authority")
	}
}
