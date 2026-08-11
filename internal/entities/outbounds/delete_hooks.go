package outbounds

import (
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

type DeleteHook func(*gorm.DB, string) error

const maxDeleteHooks = 128

type registeredDeleteHook struct {
	hook  DeleteHook
	token uint64
}

var deleteHooks = struct {
	sync.RWMutex
	entries   map[string]registeredDeleteHook
	nextToken uint64
}{
	entries: map[string]registeredDeleteHook{},
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
	if len(deleteHooks.entries) >= maxDeleteHooks {
		deleteHooks.Unlock()
		panic("outbound delete hook registry capacity exceeded")
	}
	deleteHooks.nextToken++
	token := deleteHooks.nextToken
	deleteHooks.entries[name] = registeredDeleteHook{hook: fn, token: token}
	deleteHooks.Unlock()

	return func() {
		deleteHooks.Lock()
		if current, ok := deleteHooks.entries[name]; ok && current.token == token {
			delete(deleteHooks.entries, name)
		}
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
	deleteHooks.entries = map[string]registeredDeleteHook{}
	deleteHooks.Unlock()
}

type deleteHookEntry struct {
	name string
	fn   DeleteHook
}

func deleteHookSnapshot() []deleteHookEntry {
	deleteHooks.RLock()
	entries := make([]deleteHookEntry, 0, len(deleteHooks.entries))
	for name, registered := range deleteHooks.entries {
		entries = append(entries, deleteHookEntry{name: name, fn: registered.hook})
	}
	deleteHooks.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}
