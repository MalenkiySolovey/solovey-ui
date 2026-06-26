package runtime

import (
	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

func (c *Core) Start(sbConfig []byte) error {
	var opt option.Options
	ctx := c.GetCtx()
	err := opt.UnmarshalJSONContext(ctx, sbConfig)
	if err != nil {
		// Returning the error is essential: otherwise a zero/partial option set can
		// make the caller mark the core as running while no inbound is listening.
		logger.Error("Unmarshal config err:", err.Error())
		return err
	}

	instance, err := corebox.NewBox(corebox.Options{
		Context:    ctx,
		Options:    opt,
		IPObserver: c.ipObserver,
	})
	if err != nil {
		return err
	}

	err = instance.Start()
	if err != nil {
		_ = instance.Close()
		return err
	}

	ctx = service.ContextWith(ctx, c)

	c.access.Lock()
	c.ctx = ctx
	c.instance = instance
	c.isRunning = true
	c.inboundManager = instance.Inbound()
	c.outboundManager = instance.Outbound()
	c.serviceManager = instance.Service()
	c.endpointManager = instance.Endpoint()
	c.router = instance.Router()
	c.factory = instance.LogFactory()
	c.access.Unlock()
	return nil
}

func (c *Core) Stop() error {
	c.access.Lock()
	c.isRunning = false
	if c.instance == nil {
		c.access.Unlock()
		return nil
	}
	instance := c.instance
	c.instance = nil
	c.inboundManager = nil
	c.outboundManager = nil
	c.serviceManager = nil
	c.endpointManager = nil
	c.router = nil
	c.factory = nil
	c.access.Unlock()
	err := instance.Close()
	return err
}
