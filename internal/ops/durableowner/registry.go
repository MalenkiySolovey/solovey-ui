// Package durableowner is the neutral installed-owner manifest catalog. It is
// independent from component runtime/enabled state so migrations, backup and
// restore can reason about disabled durable owners without importing the
// component host.
package durableowner

import (
	"reflect"
	"sort"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

var catalog = struct {
	sync.RWMutex
	items map[string]manifest.Manifest
}{items: map[string]manifest.Manifest{}}

func Register(item manifest.Manifest) {
	item = cloneManifest(item).Normalized()
	item = cloneManifest(item)
	if err := item.Validate(); err != nil {
		panic(err)
	}
	catalog.Lock()
	defer catalog.Unlock()
	if existing, ok := catalog.items[item.ID]; ok {
		if !reflect.DeepEqual(existing, item) {
			panic("durable owner manifest identity changed")
		}
		return
	}
	catalog.items[item.ID] = item
}

func Lookup(id string) (manifest.Manifest, bool) {
	catalog.RLock()
	defer catalog.RUnlock()
	item, ok := catalog.items[id]
	return cloneManifest(item), ok
}

func Manifests() []manifest.Manifest {
	catalog.RLock()
	defer catalog.RUnlock()
	result := make([]manifest.Manifest, 0, len(catalog.items))
	for _, item := range catalog.items {
		result = append(result, cloneManifest(item))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func cloneManifest(item manifest.Manifest) manifest.Manifest {
	item.TokenScopes = append([]string(nil), item.TokenScopes...)
	item.Frontend.Entries = append([]string(nil), item.Frontend.Entries...)
	item.Database.Tables = append([]string(nil), item.Database.Tables...)
	item.Database.Settings = append([]string(nil), item.Database.Settings...)
	item.Database.Secrets = append([]string(nil), item.Database.Secrets...)
	item.Database.Files = append([]manifest.DurableFileResource(nil), item.Database.Files...)
	return item
}
