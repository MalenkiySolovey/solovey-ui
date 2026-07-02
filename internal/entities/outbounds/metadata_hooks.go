package outbounds

import (
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

type MetadataAnnotator func(*gorm.DB, []map[string]interface{}) error

var metadataAnnotators = struct {
	sync.RWMutex
	entries map[string]MetadataAnnotator
}{
	entries: map[string]MetadataAnnotator{},
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
	metadataAnnotators.entries[name] = fn
	metadataAnnotators.Unlock()

	return func() {
		metadataAnnotators.Lock()
		delete(metadataAnnotators.entries, name)
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

func ResetMetadataAnnotatorsForTest() {
	metadataAnnotators.Lock()
	metadataAnnotators.entries = map[string]MetadataAnnotator{}
	metadataAnnotators.Unlock()
}

type metadataAnnotatorEntry struct {
	name string
	fn   MetadataAnnotator
}

func metadataAnnotatorSnapshot() []metadataAnnotatorEntry {
	metadataAnnotators.RLock()
	entries := make([]metadataAnnotatorEntry, 0, len(metadataAnnotators.entries))
	for name, fn := range metadataAnnotators.entries {
		entries = append(entries, metadataAnnotatorEntry{name: name, fn: fn})
	}
	metadataAnnotators.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}
