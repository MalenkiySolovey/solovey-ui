package database

import (
	"context"
	"sort"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type importPostOpenHook func(context.Context) error

var importPostOpenHooks = struct {
	sync.Mutex
	byName map[string]importPostOpenHook
}{
	byName: map[string]importPostOpenHook{},
}

func RegisterImportPostOpenHook(name string, fn func(context.Context) error) {
	if name == "" {
		return
	}
	importPostOpenHooks.Lock()
	defer importPostOpenHooks.Unlock()
	if fn == nil {
		delete(importPostOpenHooks.byName, name)
		return
	}
	importPostOpenHooks.byName[name] = fn
}

func runImportPostOpenHooks(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	importPostOpenHooks.Lock()
	names := make([]string, 0, len(importPostOpenHooks.byName))
	for name := range importPostOpenHooks.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	hooks := make([]struct {
		name string
		run  importPostOpenHook
	}, 0, len(names))
	for _, name := range names {
		hooks = append(hooks, struct {
			name string
			run  importPostOpenHook
		}{name: name, run: importPostOpenHooks.byName[name]})
	}
	importPostOpenHooks.Unlock()

	for _, hook := range hooks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := hook.run(ctx); err != nil {
			return common.NewErrorf("Error running import post-open hook %s: %v", hook.name, err)
		}
	}
	return ctx.Err()
}
