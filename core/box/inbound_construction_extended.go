//go:build shtorm_extended

package box

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
)

// The native manager removes failed candidates from their private Scope. It
// preserves the actual inbound and its optional interfaces without the legacy
// manager's additional unpublished-candidate cleanup adapter.
type constructionRegistry struct{ adapter.InboundRegistry }

func BeginInboundConstruction(ctx context.Context, _ string) (context.Context, func(error) error) {
	return ctx, func(err error) error { return err }
}
