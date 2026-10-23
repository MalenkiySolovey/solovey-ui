package registry

import (
	"context"

	"github.com/sagernet/sing/service"
)

// Context preserves cancellation and explicit context values, but starts a new
// service registry and option registries. No mutable manager from a live box is
// inherited by a validation or replacement generation.
func Context(parent context.Context) context.Context {
	ctx := service.ContextWithRegistry(parent, service.NewRegistry())
	return nativeContext(ctx)
}
