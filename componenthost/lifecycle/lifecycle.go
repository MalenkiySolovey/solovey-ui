package lifecycle

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
)

type Context struct {
	Host componenthost.Deps
}

type Lifecycle interface {
	Start(context.Context, Context) error
	Stop(context.Context) error
}

type Migrator interface {
	Migrate(context.Context, Context) error
}

type Noop struct{}

func (Noop) Start(context.Context, Context) error {
	return nil
}

func (Noop) Stop(context.Context) error {
	return nil
}
