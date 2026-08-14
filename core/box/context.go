package box

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/core/tracker"

	"github.com/sagernet/sing-box/option"
)

type Options struct {
	option.Options
	Context    context.Context
	IPObserver tracker.IPObserver
}
