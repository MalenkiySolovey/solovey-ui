package runtime

import (
	"context"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
)

func (c *Core) GetCtx() context.Context {
	c.access.RLock()
	defer c.access.RUnlock()
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *Core) GetInstance() *corebox.Box {
	c.access.RLock()
	defer c.access.RUnlock()
	return c.instance
}

func (c *Core) IsRunning() bool {
	c.access.RLock()
	defer c.access.RUnlock()
	return c.isRunning
}
