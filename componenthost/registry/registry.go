package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"

	"github.com/gin-gonic/gin"
)

type Component struct {
	Manifest  manifest.Manifest
	Lifecycle lifecycle.Lifecycle
}

type APIRouteRegistrar func(*gin.RouterGroup, componenthost.APIDeps) error

type APIRoutes struct {
	ComponentID string
	Register    APIRouteRegistrar
}

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
	defaultRegistry.register(component)
}

func RegisterAPIRoutes(componentID string, register APIRouteRegistrar) {
	defaultRegistry.registerAPIRoutes(componentID, register)
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
	if err := component.Manifest.Validate(); err != nil {
		panic(err)
	}
	if component.Lifecycle == nil {
		panic(fmt.Errorf("component %q lifecycle is required", component.Manifest.ID))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.components[component.Manifest.ID]; exists {
		panic(fmt.Errorf("component %q already registered", component.Manifest.ID))
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
		components = append(components, component)
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
	return component, ok
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
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i].Manifest.ID < components[j].Manifest.ID
	})
	return components
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
