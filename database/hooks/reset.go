package hooks

import (
	"context"
	"sort"
	"sync"
)

var resetHooks = struct {
	sync.Mutex
	byName map[string]func()
}{
	byName: map[string]func(){},
}

const maxResetHooks = 128

func RegisterResetHook(name string, fn func()) {
	if name == "" {
		return
	}
	resetHooks.Lock()
	defer resetHooks.Unlock()
	if fn == nil {
		delete(resetHooks.byName, name)
		return
	}
	if _, exists := resetHooks.byName[name]; !exists && len(resetHooks.byName) >= maxResetHooks {
		panic("database reset-hook registry capacity exceeded")
	}
	resetHooks.byName[name] = fn
}

func ResetCaches(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	resetHooks.Lock()
	names := make([]string, 0, len(resetHooks.byName))
	for name := range resetHooks.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	hooks := make([]func(), 0, len(names))
	for _, name := range names {
		hooks = append(hooks, resetHooks.byName[name])
	}
	resetHooks.Unlock()

	for _, hook := range hooks {
		if err := ctx.Err(); err != nil {
			return err
		}
		hook()
	}
	return ctx.Err()
}
