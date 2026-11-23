package box

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"

	"github.com/MalenkiySolovey/solovey-ui/core/tracker"

	"github.com/sagernet/sing-box/option"
)

type Options struct {
	IdentityBindings []inboundidentity.Binding
	PreparedTUN      *PreparedTUN
	option.Options
	Context    context.Context
	IPObserver tracker.IPObserver
}
