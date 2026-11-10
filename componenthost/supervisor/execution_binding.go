package supervisor

import (
	"context"

	canonical "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
)

// localLifecycle retains the accepted Panel binding. Typed Components still
// receive only their declared ports through the existing lifecycle adapter.
type localLifecycle struct {
	component registry.Component
	host      componenthost.Deps
}

func (l localLifecycle) Start(ctx context.Context, _ canonical.Ports) error {
	return l.component.Lifecycle.Start(ctx, lifecycle.Context{Host: l.host})
}
func (l localLifecycle) Stop(ctx context.Context) error { return l.component.Lifecycle.Stop(ctx) }

func (s *Supervisor) lifecycleEntries(components []registry.Component) []canonical.Entry {
	entries := make([]canonical.Entry, 0, len(components))
	for _, component := range components {
		entries = append(entries, canonical.Entry{ID: component.Manifest.ID,
			Lifecycle: localLifecycle{component: component, host: s.host}, Ports: canonical.Ports{Host: hostref.Local()}})
	}
	return entries
}
func componentsFromEntries(entries []canonical.Entry) []registry.Component {
	components := make([]registry.Component, 0, len(entries))
	for _, entry := range entries {
		// The shared loop returns only supplied entries, preserving their
		// lifecycle value and the actual retained Component instance.
		components = append(components, entry.Lifecycle.(localLifecycle).component)
	}
	return components
}
