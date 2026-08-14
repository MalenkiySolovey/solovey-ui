package state

import (
	"fmt"
	"sort"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

type Component struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	Since          string            `json:"since,omitempty"`
	Delivery       manifest.Delivery `json:"delivery"`
	DefaultEnabled bool              `json:"defaultEnabled"`
	TokenScopes    []string          `json:"tokenScopes,omitempty"`
	Installed      bool              `json:"installed"`
	Enabled        bool              `json:"enabled"`
	Active         bool              `json:"active"`
}

var activeCache = struct {
	sync.RWMutex
	ids        map[string]struct{}
	generation uint64
}{}

func Components() ([]Component, error) {
	installedComponents, err := installstate.InstalledComponents()
	if err != nil {
		return nil, err
	}
	manifests := make([]manifest.Manifest, 0, len(installedComponents))
	for _, item := range installedComponents {
		component, ok := registry.ComponentByID(item.ID)
		if !ok {
			continue
		}
		if item.Delivery != "" && item.Delivery != component.Manifest.Delivery {
			return nil, fmt.Errorf("installed component %q delivery %q does not match binary delivery %q", item.ID, item.Delivery, component.Manifest.Delivery)
		}
		manifests = append(manifests, component.Manifest)
	}
	enabled, err := enabledstate.EnabledIDs(manifests)
	if err != nil {
		return nil, err
	}

	result := make([]Component, 0, len(manifests))
	for _, item := range manifests {
		_, isEnabled := enabled[item.ID]
		result = append(result, Component{
			ID:             item.ID,
			Name:           item.Name,
			Version:        item.Version,
			Since:          item.Since,
			Delivery:       item.Delivery,
			DefaultEnabled: item.DefaultEnabled,
			TokenScopes:    append([]string(nil), item.TokenScopes...),
			Installed:      true,
			Enabled:        isEnabled,
			Active:         isEnabled,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func ActiveIDs() (map[string]struct{}, error) {
	components, err := Components()
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(components))
	for _, component := range components {
		if component.Active {
			ids[component.ID] = struct{}{}
		}
	}
	return ids, nil
}

func IsActiveCached(id string) (bool, error) {
	ids, err := activeIDsCachedSnapshot()
	if err != nil {
		return false, err
	}
	_, ok := ids[id]
	return ok, nil
}

func activeIDsCachedSnapshot() (map[string]struct{}, error) {
	for {
		activeCache.RLock()
		if activeCache.ids != nil {
			ids := activeCache.ids
			activeCache.RUnlock()
			return ids, nil
		}
		generation := activeCache.generation
		activeCache.RUnlock()

		ids, err := ActiveIDs()
		if err != nil {
			return nil, err
		}
		if cached, published := publishActiveIDs(generation, ids); published {
			return cached, nil
		}
		// Installed/enabled state changed while the snapshot was loading. Do
		// not resurrect the stale snapshot after invalidation; load the new
		// generation instead.
	}
}

func publishActiveIDs(generation uint64, ids map[string]struct{}) (map[string]struct{}, bool) {
	activeCache.Lock()
	defer activeCache.Unlock()
	if activeCache.generation != generation {
		return nil, false
	}
	if activeCache.ids == nil {
		activeCache.ids = cloneIDs(ids)
	}
	cached := activeCache.ids
	return cached, true
}

func InvalidateActiveCache() {
	activeCache.Lock()
	activeCache.generation++
	activeCache.ids = nil
	activeCache.Unlock()
}

func InstalledIDs() (map[string]struct{}, error) {
	components, err := Components()
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(components))
	for _, component := range components {
		if component.Installed {
			ids[component.ID] = struct{}{}
		}
	}
	return ids, nil
}

func cloneIDs(ids map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(ids))
	for id := range ids {
		cloned[id] = struct{}{}
	}
	return cloned
}
