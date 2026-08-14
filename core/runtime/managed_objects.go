package runtime

import (
	"errors"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

func (c *Core) AddInbound(config []byte) error {
	return c.withMutation(func(rt coreRuntime) error {
		var inboundConfig option.Inbound
		if err := inboundConfig.UnmarshalJSONContext(rt.ctx, config); err != nil {
			return err
		}
		if err := rt.inboundManager.Create(
			rt.ctx,
			rt.router,
			rt.factory.NewLogger("inbound/"+inboundConfig.Type+"["+inboundConfig.Tag+"]"),
			inboundConfig.Tag,
			inboundConfig.Type,
			inboundConfig.Options); err != nil {
			return err
		}
		record, recorded := inboundRuntimeRecord(rt.ctx, inboundConfig, 0)
		if !recorded {
			return errors.Join(errors.New("record effective inbound"), rt.inboundManager.Remove(inboundConfig.Tag))
		}
		c.access.Lock()
		if c.effectiveInbounds == nil {
			c.effectiveInbounds = make(map[string]InboundRuntimeRecord)
		}
		record.ManagerGeneration = c.managerGeneration
		c.effectiveInbounds[inboundConfig.Tag] = record
		c.access.Unlock()
		return nil
	})
}

func (c *Core) RemoveInbound(tag string) error {
	return c.withMutation(func(rt coreRuntime) error {
		logger.Info("remove inbound: ", tag)
		if err := rt.inboundManager.Remove(tag); err != nil {
			return err
		}
		c.access.Lock()
		if c.effectiveInbounds != nil {
			delete(c.effectiveInbounds, tag)
		}
		c.access.Unlock()
		return nil
	})
}

func (c *Core) AddOutbound(config []byte) error {
	return c.withMutation(func(rt coreRuntime) error {
		var outboundConfig option.Outbound
		if err := outboundConfig.UnmarshalJSONContext(rt.ctx, config); err != nil {
			return err
		}
		outboundCtx := adapter.WithContext(rt.ctx, &adapter.InboundContext{Outbound: outboundConfig.Tag})
		return rt.outboundManager.Create(
			outboundCtx,
			rt.router,
			rt.factory.NewLogger("outbound/"+outboundConfig.Type+"["+outboundConfig.Tag+"]"),
			outboundConfig.Tag,
			outboundConfig.Type,
			outboundConfig.Options)
	})
}

func (c *Core) RemoveOutbound(tag string) error {
	return c.withMutation(func(rt coreRuntime) error {
		logger.Info("remove outbound: ", tag)
		return rt.outboundManager.Remove(tag)
	})
}

func (c *Core) AddEndpoint(config []byte) error {
	return c.withMutation(func(rt coreRuntime) error {
		var endpointConfig option.Endpoint
		if err := endpointConfig.UnmarshalJSONContext(rt.ctx, config); err != nil {
			return err
		}
		return rt.endpointManager.Create(
			rt.ctx,
			rt.router,
			rt.factory.NewLogger("endpoint/"+endpointConfig.Type+"["+endpointConfig.Tag+"]"),
			endpointConfig.Tag,
			endpointConfig.Type,
			endpointConfig.Options)
	})
}

func (c *Core) RemoveEndpoint(tag string) error {
	return c.withMutation(func(rt coreRuntime) error {
		logger.Info("remove endpoint: ", tag)
		return rt.endpointManager.Remove(tag)
	})
}

func (c *Core) AddService(config []byte) error {
	return c.withMutation(func(rt coreRuntime) error {
		var serviceConfig option.Service
		if err := serviceConfig.UnmarshalJSONContext(rt.ctx, config); err != nil {
			return err
		}
		return rt.serviceManager.Create(
			rt.ctx,
			rt.factory.NewLogger("service/"+serviceConfig.Type+"["+serviceConfig.Tag+"]"),
			serviceConfig.Tag,
			serviceConfig.Type,
			serviceConfig.Options)
	})
}

func (c *Core) RemoveService(tag string) error {
	return c.withMutation(func(rt coreRuntime) error {
		logger.Info("remove service: ", tag)
		return rt.serviceManager.Remove(tag)
	})
}
