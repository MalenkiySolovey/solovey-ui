package registry

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/actions"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestComponentActionAdmissionRejectsUnownedAndCoreActions(t *testing.T) {
	for _, action := range []string{"future.execute", actions.ComponentRemove, actions.BackupRestore, actions.ProviderMaintenance} {
		t.Run(action, func(t *testing.T) {
			component := Component{Manifest: manifest.Manifest{ID: "telegram", Name: "Telegram", Version: "1", Delivery: manifest.DeliveryInProcess, Actions: []string{action}}, Lifecycle: lifecycle.Noop{}}
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
}
