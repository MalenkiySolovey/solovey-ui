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
	parentContext := ctx
	c.access.RUnlock()
	if alreadyRunning {
		return ErrAlreadyRunning
	}
	c.access.Lock()
	c.lifecycleState = "starting"
	c.access.Unlock()
	defer func() {
		c.access.Lock()
		if !c.isRunning {
			c.lifecycleState = "stopped_by_error"
		}
		c.access.Unlock()
	}()
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
	prepare := c.prepareAPI
	if prepare == nil {
		prepare = preparePrivateAPI
	}
	ctx, api, err := prepare(ctx, &opt)
	if err != nil {
		return err
	}
	accepted := false
	defer func() {
		if !accepted {
			api.close()
		}
	}()

	instance, err := corebox.NewBox(corebox.Options{
		Context:    ctx,
		Options:    opt,
		IPObserver: c.ipObserver,
	})
	if err != nil {
		return api.safeError(err)
	}

	if err := api.releaseReservation(); err != nil {
		return api.safeError(errors.Join(err, instance.Close()))
	}
	err = instance.Start()
	if err != nil {
		return api.safeError(errors.Join(err, instance.Close()))
	}
	if err := api.connect(instance.Context()); err != nil {
		return api.safeError(errors.Join(err, instance.Close()))
	}

	c.access.Lock()
	c.probeHealth.reset()
	c.managerGeneration++
	generation := c.managerGeneration
	c.ctx = instance.Context()
	c.parentContext = parentContext
	c.instance = instance
	c.privateAPI = api
	c.isRunning = true
	c.lifecycleState = "running"
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
	accepted = true
	return nil
}

func (c *Core) Stop() error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()

	c.access.Lock()
	c.probeHealth.reset()
	c.isRunning = false
	c.lifecycleState = "stopping"
	if c.instance == nil {
		c.lifecycleState = "stopped"
		c.access.Unlock()
		return nil
	}
	instance := c.instance
	api := c.privateAPI
	c.privateAPI = nil
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
	if api != nil {
		api.close()
	}
	err := instance.Close()
	c.access.Lock()
	if err != nil {
		c.lifecycleState = "stopped_by_error"
	} else {
		c.lifecycleState = "stopped"
	}
	c.access.Unlock()
	return err
}
