package service

import (
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

type OutboundSaveHook func(*gorm.DB) error

var outboundSaveHooks = struct {
	sync.RWMutex
	entries map[string]OutboundSaveHook
}{
	entries: map[string]OutboundSaveHook{},
}

func RegisterOutboundSaveHook(name string, fn OutboundSaveHook) func() {
	if name == "" {
		panic("outbound save hook name is required")
	}
	if fn == nil {
		panic(fmt.Errorf("outbound save hook %q is nil", name))
	}

	outboundSaveHooks.Lock()
	if _, exists := outboundSaveHooks.entries[name]; exists {
		outboundSaveHooks.Unlock()
		panic(fmt.Errorf("outbound save hook %q already registered", name))
	}
	outboundSaveHooks.entries[name] = fn
	outboundSaveHooks.Unlock()

	return func() {
		outboundSaveHooks.Lock()
		delete(outboundSaveHooks.entries, name)
		outboundSaveHooks.Unlock()
	}
}

func runOutboundSaveHooks(tx *gorm.DB) error {
	for _, hook := range outboundSaveHookSnapshot() {
		if err := hook.fn(tx); err != nil {
			return fmt.Errorf("outbound save hook %q failed: %w", hook.name, err)
		}
	}
	return nil
}

func ResetOutboundSaveHooksForTest() {
	outboundSaveHooks.Lock()
	outboundSaveHooks.entries = map[string]OutboundSaveHook{}
	outboundSaveHooks.Unlock()
}

type outboundSaveHookEntry struct {
	name string
	fn   OutboundSaveHook
}

func outboundSaveHookSnapshot() []outboundSaveHookEntry {
	outboundSaveHooks.RLock()
	entries := make([]outboundSaveHookEntry, 0, len(outboundSaveHooks.entries))
	for name, fn := range outboundSaveHooks.entries {
		entries = append(entries, outboundSaveHookEntry{name: name, fn: fn})
	}
	outboundSaveHooks.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}
