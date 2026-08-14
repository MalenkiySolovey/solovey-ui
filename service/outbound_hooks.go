package service

import (
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

type OutboundSaveHook func(*gorm.DB) error

const maxOutboundSaveHooks = 128

type registeredOutboundSaveHook struct {
	hook  OutboundSaveHook
	token uint64
}

var outboundSaveHooks = struct {
	sync.RWMutex
	entries   map[string]registeredOutboundSaveHook
	nextToken uint64
}{
	entries: map[string]registeredOutboundSaveHook{},
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
	if len(outboundSaveHooks.entries) >= maxOutboundSaveHooks {
		outboundSaveHooks.Unlock()
		panic("outbound save hook registry capacity exceeded")
	}
	outboundSaveHooks.nextToken++
	token := outboundSaveHooks.nextToken
	outboundSaveHooks.entries[name] = registeredOutboundSaveHook{hook: fn, token: token}
	outboundSaveHooks.Unlock()

	return func() {
		outboundSaveHooks.Lock()
		if current, ok := outboundSaveHooks.entries[name]; ok && current.token == token {
			delete(outboundSaveHooks.entries, name)
		}
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

type outboundSaveHookEntry struct {
	name string
	fn   OutboundSaveHook
}

func outboundSaveHookSnapshot() []outboundSaveHookEntry {
	outboundSaveHooks.RLock()
	entries := make([]outboundSaveHookEntry, 0, len(outboundSaveHooks.entries))
	for name, registered := range outboundSaveHooks.entries {
		entries = append(entries, outboundSaveHookEntry{name: name, fn: registered.hook})
	}
	outboundSaveHooks.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}
