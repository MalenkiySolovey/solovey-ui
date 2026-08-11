package registry

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestRegistryComponentsAreSortedAndUnique(t *testing.T) {
	r := newRegistry()
	r.register(Component{
		Manifest:  manifest.Manifest{ID: "fixture-beta", Name: "Fixture Beta", Delivery: manifest.DeliveryInProcess},
		Lifecycle: lifecycle.Noop{},
	})
	r.register(Component{
		Manifest:  manifest.Manifest{ID: "fixture-alpha", Name: "Fixture Alpha", Delivery: manifest.DeliveryInProcess},
		Lifecycle: lifecycle.Noop{},
	})

	components := r.componentsList()
	if len(components) != 2 {
		t.Fatalf("components len=%d, want 2", len(components))
	}
	if components[0].Manifest.ID != "fixture-alpha" || components[1].Manifest.ID != "fixture-beta" {
		t.Fatalf("components are not sorted by id: %#v", components)
	}

	assertPanic(t, func() {
		r.register(Component{
			Manifest:  manifest.Manifest{ID: "fixture-beta", Name: "Fixture Beta duplicate", Delivery: manifest.DeliveryInProcess},
			Lifecycle: lifecycle.Noop{},
		})
	})
}

func TestRegistryRejectsBrokenComponent(t *testing.T) {
	r := newRegistry()
	assertPanic(t, func() {
		r.register(Component{
			Manifest:  manifest.Manifest{ID: "bad id", Name: "Bad", Delivery: manifest.DeliveryInProcess},
			Lifecycle: lifecycle.Noop{},
		})
	})
	assertPanic(t, func() {
		r.register(Component{
			Manifest: manifest.Manifest{ID: "missing-lifecycle", Name: "Missing Lifecycle", Delivery: manifest.DeliveryInProcess},
		})
	})
}

func assertPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
