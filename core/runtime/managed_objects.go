package runtime

import (
	"errors"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

func (c *Core) AddInbound(config []byte) error {
	return c.AddInboundWithBindings(config, nil)
}

func (c *Core) AddInboundWithBindings(config []byte, bindings []inboundidentity.Binding) error {
	return c.withMutation(func(rt coreRuntime) error {
		var inboundConfig option.Inbound
		if err := inboundConfig.UnmarshalJSONContext(rt.ctx, config); err != nil {
			return err
		}
		if inboundConfig.Tag == privateAPITag {
			return errors.New("reserved runtime tag")
		}
		record, recorded := inboundRuntimeRecord(rt.ctx, inboundConfig, 0)
		if !recorded {
			return errors.New("record effective inbound")
		}
		boundRouter, epoch, err := rt.inboundIdentity.Prepare(rt.router, inboundConfig.Tag, bindings)
		if err != nil {
			return err
		}
		constructionCtx, finish := corebox.BeginInboundConstruction(rt.ctx, inboundConfig.Tag)
		if err := finish(rt.inboundManager.Create(
			constructionCtx,
			boundRouter,
			rt.factory.NewLogger("inbound/"+inboundConfig.Type+"["+inboundConfig.Tag+"]"),
			inboundConfig.Tag,
			inboundConfig.Type,
			inboundConfig.Options)); err != nil {
			return err
		}
		rt.inboundIdentity.Publish(inboundConfig.Tag, epoch)
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
		rt.inboundIdentity.Revoke(tag)
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
		if outboundConfig.Tag == privateAPITag {
			return errors.New("reserved runtime tag")
		}
		outboundCtx := adapter.WithContext(rt.ctx, &adapter.InboundContext{Outbound: outboundConfig.Tag})
		c.probeHealth.remove(outboundConfig.Tag)
		if err := rt.outboundManager.Create(
			outboundCtx,
			rt.router,
			rt.factory.NewLogger("outbound/"+outboundConfig.Type+"["+outboundConfig.Tag+"]"),
			outboundConfig.Tag,
			outboundConfig.Type,
			outboundConfig.Options); err != nil {
			return err
		}
		return nil
	})
}

func (c *Core) RemoveOutbound(tag string) error {
	return c.withMutation(func(rt coreRuntime) error {
		logger.Info("remove outbound: ", tag)
		c.probeHealth.remove(tag)
		if err := rt.outboundManager.Remove(tag); err != nil {
			return err
		}
		return nil
	})
}

func (c *Core) AddEndpoint(config []byte) error {
	return c.withMutation(func(rt coreRuntime) error {
		var endpointConfig option.Endpoint
		if err := endpointConfig.UnmarshalJSONContext(rt.ctx, config); err != nil {
			return err
		}
		if endpointConfig.Tag == privateAPITag {
			return errors.New("reserved runtime tag")
		}
		c.probeHealth.remove(endpointConfig.Tag)
		if err := rt.endpointManager.Create(
			rt.ctx,
			rt.router,
			rt.factory.NewLogger("endpoint/"+endpointConfig.Type+"["+endpointConfig.Tag+"]"),
			endpointConfig.Tag,
			endpointConfig.Type,
			endpointConfig.Options); err != nil {
			return err
		}
		return nil
	})
}

func (c *Core) RemoveEndpoint(tag string) error {
	return c.withMutation(func(rt coreRuntime) error {
		logger.Info("remove endpoint: ", tag)
		c.probeHealth.remove(tag)
		if err := rt.endpointManager.Remove(tag); err != nil {
			return err
		}
		return nil
	})
}

func (c *Core) AddService(config []byte) error {
	return c.withMutation(func(rt coreRuntime) error {
		var serviceConfig option.Service
		if err := serviceConfig.UnmarshalJSONContext(rt.ctx, config); err != nil {
			return err
		}
		if serviceConfig.Type == "api" || serviceConfig.Tag == privateAPITag {
			return errors.New("private runtime API is lifecycle-owned")
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
	if tag == privateAPITag {
		return errors.New("private runtime API is lifecycle-owned")
	}
	return c.withMutation(func(rt coreRuntime) error {
		logger.Info("remove service: ", tag)
		return rt.serviceManager.Remove(tag)
	})
}
