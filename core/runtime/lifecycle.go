package runtime

import (
	"errors"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

func (c *Core) Start(sbConfig []byte) error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()

	c.access.RLock()
	alreadyRunning := c.isRunning || c.instance != nil
	ctx := c.ctx
	c.access.RUnlock()
	if alreadyRunning {
		return ErrAlreadyRunning
	}
	if _, err := singboxvalidation.ValidateRuleConditions(sbConfig); err != nil {
		return err
	}
	ctx = registry.Context(ctx)
	service.MustRegister(ctx, c)

	var opt option.Options
	err := opt.UnmarshalJSONContext(ctx, sbConfig)
	if err != nil {
		// Returning the error is essential: otherwise a zero/partial option set can
		// make the caller mark the core as running while no inbound is listening.
		logger.Error("Unable to decode core configuration")
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
		return errors.Join(err, instance.Close())
	}

	c.access.Lock()
	c.probeHealth.reset()
	c.managerGeneration++
	generation := c.managerGeneration
	c.ctx = instance.Context()
	c.parentContext = ctx
	c.instance = instance
	c.isRunning = true
	c.inboundManager = instance.Inbound()
	c.outboundManager = instance.Outbound()
	c.serviceManager = instance.Service()
	c.endpointManager = instance.Endpoint()
	c.router = instance.Router()
	c.factory = instance.LogFactory()
	c.statsTracker = instance.StatsTracker()
	c.connTracker = instance.ConnTracker()
	c.effectiveInbounds = make(map[string]InboundRuntimeRecord, len(opt.Inbounds))
	for _, inboundOptions := range opt.Inbounds {
		record, recorded := inboundRuntimeRecord(ctx, inboundOptions, generation)
		if !recorded {
			continue
		}
		c.effectiveInbounds[inboundOptions.Tag] = record
	}
	c.access.Unlock()
	return nil
}

func (c *Core) Stop() error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()

	c.access.Lock()
	c.probeHealth.reset()
	c.isRunning = false
	if c.instance == nil {
		c.access.Unlock()
		return nil
	}
	instance := c.instance
	c.ctx = c.parentContext
	c.instance = nil
	c.inboundManager = nil
	c.outboundManager = nil
	c.serviceManager = nil
	c.endpointManager = nil
	c.router = nil
	c.factory = nil
	c.statsTracker = nil
	c.connTracker = nil
	c.effectiveInbounds = make(map[string]InboundRuntimeRecord)
	c.access.Unlock()
	err := instance.Close()
	return err
}
