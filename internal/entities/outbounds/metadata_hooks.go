package outbounds

import (
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

type MetadataAnnotator func(*gorm.DB, []map[string]interface{}) error

const maxMetadataAnnotators = 128

type registeredMetadataAnnotator struct {
	annotator MetadataAnnotator
	token     uint64
}

var metadataAnnotators = struct {
	sync.RWMutex
	entries   map[string]registeredMetadataAnnotator
	nextToken uint64
}{
	entries: map[string]registeredMetadataAnnotator{},
}

func RegisterMetadataAnnotator(name string, fn MetadataAnnotator) func() {
	if name == "" {
		panic("outbound metadata annotator name is required")
	}
	if fn == nil {
		panic(fmt.Errorf("outbound metadata annotator %q is nil", name))
	}

	metadataAnnotators.Lock()
	if _, exists := metadataAnnotators.entries[name]; exists {
		metadataAnnotators.Unlock()
		panic(fmt.Errorf("outbound metadata annotator %q already registered", name))
	}
	if len(metadataAnnotators.entries) >= maxMetadataAnnotators {
		metadataAnnotators.Unlock()
		panic("outbound metadata annotator registry capacity exceeded")
	}
	metadataAnnotators.nextToken++
	token := metadataAnnotators.nextToken
	metadataAnnotators.entries[name] = registeredMetadataAnnotator{annotator: fn, token: token}
	metadataAnnotators.Unlock()

	return func() {
		metadataAnnotators.Lock()
		if current, ok := metadataAnnotators.entries[name]; ok && current.token == token {
			delete(metadataAnnotators.entries, name)
		}
		metadataAnnotators.Unlock()
	}
}

func annotateMetadata(tx *gorm.DB, outbounds []map[string]interface{}) error {
	for _, annotator := range metadataAnnotatorSnapshot() {
		if err := annotator.fn(tx, outbounds); err != nil {
			return fmt.Errorf("outbound metadata annotator %q failed: %w", annotator.name, err)
		}
	}
	return nil
}

type metadataAnnotatorEntry struct {
	name string
	fn   MetadataAnnotator
}

func metadataAnnotatorSnapshot() []metadataAnnotatorEntry {
	metadataAnnotators.RLock()
	entries := make([]metadataAnnotatorEntry, 0, len(metadataAnnotators.entries))
	for name, registered := range metadataAnnotators.entries {
		entries = append(entries, metadataAnnotatorEntry{name: name, fn: registered.annotator})
	}
	metadataAnnotators.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}
