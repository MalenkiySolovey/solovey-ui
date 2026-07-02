package outbounds

import (
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

type DeleteHook func(*gorm.DB, string) error

var deleteHooks = struct {
	sync.RWMutex
	entries map[string]DeleteHook
}{
	entries: map[string]DeleteHook{},
}

func RegisterDeleteHook(name string, fn DeleteHook) func() {
	if name == "" {
		panic("outbound delete hook name is required")
	}
	if fn == nil {
		panic(fmt.Errorf("outbound delete hook %q is nil", name))
	}

	deleteHooks.Lock()
	if _, exists := deleteHooks.entries[name]; exists {
		deleteHooks.Unlock()
		panic(fmt.Errorf("outbound delete hook %q already registered", name))
	}
	deleteHooks.entries[name] = fn
	deleteHooks.Unlock()

	return func() {
		deleteHooks.Lock()
		delete(deleteHooks.entries, name)
		deleteHooks.Unlock()
	}
}

func runDeleteHooks(tx *gorm.DB, tag string) error {
	for _, hook := range deleteHookSnapshot() {
		if err := hook.fn(tx, tag); err != nil {
			return fmt.Errorf("outbound delete hook %q failed: %w", hook.name, err)
		}
	}
	return nil
}

func ResetDeleteHooksForTest() {
	deleteHooks.Lock()
	deleteHooks.entries = map[string]DeleteHook{}
	deleteHooks.Unlock()
}

type deleteHookEntry struct {
	name string
	fn   DeleteHook
}

func deleteHookSnapshot() []deleteHookEntry {
	deleteHooks.RLock()
	entries := make([]deleteHookEntry, 0, len(deleteHooks.entries))
	for name, fn := range deleteHooks.entries {
		entries = append(entries, deleteHookEntry{name: name, fn: fn})
	}
	deleteHooks.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}
