package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"

	"github.com/gin-gonic/gin"
)

type Component struct {
	Manifest      manifest.Manifest
	Lifecycle     lifecycle.Lifecycle
	PortLifecycle lifecycle.PortLifecycle
}

type APIRouteRegistrar func(*gin.RouterGroup, componenthost.APIDeps) error

type APIRoutes struct {
	ComponentID string
	Register    APIRouteRegistrar
}

const maxRegisteredComponents = 64

var defaultRegistry = newRegistry()

type registry struct {
	mu         sync.RWMutex
	components map[string]Component
	apiRoutes  []APIRoutes
}

func newRegistry() *registry {
	return &registry{
		components: map[string]Component{},
	}
}

func Register(component Component) {
	hooks := durableowner.Hooks{}
	if migrator, ok := component.Lifecycle.(lifecycle.StagedMigrator); ok {
		hooks.MigrateStaged = migrator.MigrateStaged
	}
	if rehearser, ok := component.Lifecycle.(lifecycle.RestoreRehearser); ok {
		hooks.RehearseRestore = rehearser.RehearseRestore
	}
	if exclusions, ok := component.Lifecycle.(lifecycle.BackupExclusions); ok {
		hooks.NonportableBackupTables = exclusions.NonportableBackupTables
	}
	defaultRegistry.registerWith(component, func(item manifest.Manifest) {
		durableowner.RegisterWithHooks(item, hooks)
	})
}

func RegisterAPIRoutes(componentID string, register APIRouteRegistrar) {
	defaultRegistry.registerLegacyAPIRoutes(componentID, register)
}

func (r *registry) registerLegacyAPIRoutes(componentID string, register APIRouteRegistrar) {
	component, ok := r.componentByID(componentID)
	if !ok || component.Manifest.HostAPI != nil {
		panic(fmt.Errorf("component %q must use its declared typed API boundary", componentID))
	}
	r.registerAPIRoutes(componentID, register)
}

// RegisterPortAPIRoutes binds only the manifest's declared request ports. Routes
// remain under the existing authenticated/CSRF-protected ComponentHost group.
func RegisterPortAPIRoutes(componentID string, register func(*gin.RouterGroup, componenthost.Ports) error) {
	defaultRegistry.registerPortAPIRoutes(componentID, register)
}

func (r *registry) registerPortAPIRoutes(componentID string, register func(*gin.RouterGroup, componenthost.Ports) error) {
	component, ok := r.componentByID(componentID)
	if !ok || component.Manifest.HostAPI == nil || register == nil {
		panic(fmt.Errorf("component %q typed API contract is required", componentID))
	}
	contract := *component.Manifest.HostAPI
	r.registerAPIRoutes(componentID, func(g *gin.RouterGroup, host componenthost.APIDeps) error {
		ports, err := host.BindAPI(contract)
		if err != nil {
			return err
		}
		return register(g, ports)
	})
}

func Components() []Component {
	return defaultRegistry.componentsList()
}

func ComponentByID(id string) (Component, bool) {
	return defaultRegistry.componentByID(id)
}

func ComponentsByID(ids []string) []Component {
	return defaultRegistry.componentsByID(ids)
}

func APIRouteRegistrarsByComponentIDs(ids map[string]struct{}) []APIRoutes {
	return defaultRegistry.apiRouteRegistrarsByComponentIDs(ids)
}

func (r *registry) register(component Component) {
	r.registerWith(component, nil)
}

func (r *registry) registerWith(component Component, admit func(manifest.Manifest)) {
	if err := component.Manifest.Validate(); err != nil {
		panic(err)
	}
	for _, actionID := range component.Manifest.Actions {
		if !actions.ComponentAllowed(component.Manifest.ID, component.Manifest.Actions, actionID) {
			panic(fmt.Errorf("component %q action %q is not admitted", component.Manifest.ID, actionID))
		}
	}
	component.Manifest = component.Manifest.Normalized()
	if component.PortLifecycle != nil {
		if component.Lifecycle != nil || component.Manifest.HostAPI == nil {
			panic(fmt.Errorf("component %q must declare one typed lifecycle contract", component.Manifest.ID))
		}
		var err error
		component.Lifecycle, err = lifecycle.BindPorts(component.Manifest, component.PortLifecycle)
		if err != nil {
			panic(fmt.Errorf("component %q: %w", component.Manifest.ID, err))
		}
	} else if component.Manifest.HostAPI != nil {
		panic(fmt.Errorf("component %q Host API requires typed lifecycle", component.Manifest.ID))
	}
	if component.Lifecycle == nil {
		panic(fmt.Errorf("component %q lifecycle is required", component.Manifest.ID))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.components[component.Manifest.ID]; exists {
		panic(fmt.Errorf("component %q already registered", component.Manifest.ID))
	}
	if len(r.components) >= maxRegisteredComponents {
		panic("component registry capacity exceeded")
	}
	if admit != nil {
		admit(component.Manifest)
	}
	r.components[component.Manifest.ID] = component
}

func (r *registry) registerAPIRoutes(componentID string, register APIRouteRegistrar) {
	if componentID == "" {
		panic("component route registrar id is required")
	}
	if register == nil {
		panic(fmt.Errorf("component %q API route registrar is required", componentID))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.apiRoutes) >= maxRegisteredComponents {
		panic("component API route registry capacity exceeded")
	}
	for _, current := range r.apiRoutes {
		if current.ComponentID == componentID {
			panic(fmt.Errorf("component %q API routes already registered", componentID))
		}
	}
	r.apiRoutes = append(r.apiRoutes, APIRoutes{
		ComponentID: componentID,
		Register:    register,
	})
}

func (r *registry) componentsList() []Component {
	r.mu.RLock()
	defer r.mu.RUnlock()

	components := make([]Component, 0, len(r.components))
	for _, component := range r.components {
		components = append(components, snapshot(component))
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i].Manifest.ID < components[j].Manifest.ID
	})
	return components
}

func (r *registry) componentByID(id string) (Component, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	component, ok := r.components[id]
	return snapshot(component), ok
}

func (r *registry) componentsByID(ids []string) []Component {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]struct{}, len(ids))
	components := make([]Component, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		component, ok := r.components[id]
		if !ok {
			continue
		}
		components = append(components, snapshot(component))
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i].Manifest.ID < components[j].Manifest.ID
	})
	return components
}

func snapshot(component Component) Component {
	component.Manifest.HostAPI = component.Manifest.HostAPI.Clone()
	component.Manifest.Actions = append([]string(nil), component.Manifest.Actions...)
	return component
}

func (r *registry) apiRouteRegistrarsByComponentIDs(ids map[string]struct{}) []APIRoutes {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := make([]APIRoutes, 0, len(r.apiRoutes))
	for _, route := range r.apiRoutes {
		if _, ok := ids[route.ComponentID]; ok {
			routes = append(routes, route)
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].ComponentID < routes[j].ComponentID
	})
	return routes
}
