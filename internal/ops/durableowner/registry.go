// Package durableowner is the neutral installed-owner manifest catalog. It is
// independent from component runtime/enabled state so migrations, backup and
// restore can reason about disabled durable owners without importing the
// component host.
package durableowner

import (
	"reflect"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

var catalog = struct {
	sync.RWMutex
	items map[string]manifest.Manifest
}{items: map[string]manifest.Manifest{}}

func Register(item manifest.Manifest) {
	item = cloneManifest(item)
	if err := item.Validate(); err != nil {
		panic(err)
	}
	item = cloneManifest(item.Normalized())
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

// RegisterWithHooks atomically publishes the durable manifest and its runtime
// restore hooks. A failed admission must not leave one half of the owner
// contract visible to backup or restore.
func RegisterWithHooks(item manifest.Manifest, hooks Hooks) {
	item = cloneManifest(item)
	if err := item.Validate(); err != nil {
		panic(err)
	}
	item = cloneManifest(item.Normalized())

	catalog.Lock()
	hookCatalog.Lock()
	defer hookCatalog.Unlock()
	defer catalog.Unlock()

	if existing, ok := catalog.items[item.ID]; ok && !reflect.DeepEqual(existing, item) {
		panic("durable owner manifest identity changed")
	}
	if _, duplicate := hookCatalog.items[item.ID]; duplicate {
		panic("durable owner hooks already registered: " + item.ID)
	}
	catalog.items[item.ID] = item
	hookCatalog.items[item.ID] = hooks
}

func Lookup(id string) (manifest.Manifest, bool) {
	catalog.RLock()
	defer catalog.RUnlock()
	item, ok := catalog.items[id]
	return cloneManifest(item), ok
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
