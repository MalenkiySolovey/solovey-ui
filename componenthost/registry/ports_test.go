package registry

import (
	"context"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/gin-gonic/gin"
)

type typedOwner struct{}

func (typedOwner) Start(context.Context, componenthost.Ports) error { return nil }
func (typedOwner) Stop(context.Context) error                       { return nil }

func typedFixture() Component {
	return Component{Manifest: manifest.Manifest{ID: "typed-owner", Name: "Typed owner", Delivery: manifest.DeliveryInProcess,
		HostAPI: &manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{manifest.PortHTTPRequest}}}, PortLifecycle: typedOwner{}}
}

func TestRegistryRejectsAmbiguousOrUndeclaredTypedLifecycleBeforeAdmission(t *testing.T) {
	for _, change := range []func(*Component){
		func(c *Component) { c.Lifecycle = lifecycle.Noop{} },
		func(c *Component) { c.Manifest.HostAPI = nil },
		func(c *Component) { c.PortLifecycle = nil; c.Lifecycle = lifecycle.Noop{} },
		func(c *Component) { c.Manifest.HostAPI.Version = "future" },
	} {
		component := typedFixture()
		change(&component)
		admitted := false
		r := newRegistry()
		assertPanic(t, func() { r.registerWith(component, func(manifest.Manifest) { admitted = true }) })
		if admitted || len(r.componentsList()) != 0 {
			t.Fatal("invalid typed registration published authority")
		}
	}
}

func TestTypedRegistryPreservesDescriptorAndRejectsLegacyAPIRouteBypass(t *testing.T) {
	r := newRegistry()
	component := typedFixture()
	r.register(component)
	component.Manifest.HostAPI.API[0] = "database"
	for _, projection := range []Component{r.componentsList()[0], r.componentsByID([]string{component.Manifest.ID})[0]} {
		projection.Manifest.HostAPI.API[0] = "runtime"
	}
	stored, _ := r.componentByID(component.Manifest.ID)
	if stored.Manifest.HostAPI.API[0] != manifest.PortHTTPRequest {
		t.Fatal("descriptor mutated through a projection")
	}
	assertPanic(t, func() {
		r.registerLegacyAPIRoutes(component.Manifest.ID, func(*gin.RouterGroup, componenthost.APIDeps) error { return nil })
	})
	assertPanic(t, func() {
		r.registerLegacyAPIRoutes("unknown", func(*gin.RouterGroup, componenthost.APIDeps) error { return nil })
	})
	assertPanic(t, func() {
		r.registerPortAPIRoutes("unknown", func(*gin.RouterGroup, componenthost.Ports) error { return nil })
	})
	called := false
	r.registerPortAPIRoutes(component.Manifest.ID, func(_ *gin.RouterGroup, ports componenthost.Ports) error {
		called = true
		if !ports.Host.IsLocal() || ports.Auth.RequireScope == nil || ports.OutboundHTTP != nil || ports.Scheduler != nil {
			t.Fatal("invalid typed route binding")
		}
		return nil
	})
	registrar := r.apiRouteRegistrarsByComponentIDs(map[string]struct{}{component.Manifest.ID: {}})[0]
	if err := registrar.Register(nil, componenthost.APIDeps{}); err == nil || called {
		t.Fatal("unavailable port reached route owner")
	}
	if err := registrar.Register(nil, componenthost.APIDeps{Auth: componenthost.AuthDeps{RequireScope: func(*gin.Context, string, ...string) bool { return true }}, HTTP: componenthost.HTTPDeps{JSONObj: func(*gin.Context, interface{}, error) {}}}); err != nil || !called {
		t.Fatal("typed registrar failed", err)
	}
}
