package hooks

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

var resetHooks = struct {
	sync.Mutex
	byName map[string]func(context.Context) error
}{
	byName: map[string]func(context.Context) error{},
}

const maxResetHooks = 128

func RegisterResetHook(name string, fn func()) {
	if fn == nil {
		RegisterContextResetHook(name, nil)
		return
	}
	RegisterContextResetHook(name, func(context.Context) error { fn(); return nil })
}

// RegisterContextResetHook lets a cached runtime owner reject restoration when
// it cannot bind to the opened database. Reset is also safe after rollback;
// unlike import post-open hooks, it must not normalize restored durable data.
func RegisterContextResetHook(name string, fn func(context.Context) error) {
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
	hooks := make([]func(context.Context) error, 0, len(names))
	for _, name := range names {
		hooks = append(hooks, resetHooks.byName[name])
	}
	resetHooks.Unlock()

	for index, hook := range hooks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := hook(ctx); err != nil {
			return fmt.Errorf("reset database cache %s: %w", names[index], err)
		}
	}
	return ctx.Err()
}
