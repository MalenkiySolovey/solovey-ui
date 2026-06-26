package validation

import (
	"context"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
)

type DryChecker struct{}

func NewDryChecker() DryChecker {
	return DryChecker{}
}

func (DryChecker) ValidateConfig(sbConfig []byte) error {
	var opt option.Options
	ctx := context.Background()
	ctx = sb.Context(
		ctx,
		registry.InboundRegistry(),
		registry.OutboundRegistry(),
		registry.EndpointRegistry(),
		registry.DNSTransportRegistry(),
		registry.ServiceRegistry(),
	)
	if err := opt.UnmarshalJSONContext(ctx, sbConfig); err != nil {
		return err
	}
	instance, err := corebox.NewBox(corebox.Options{
		Context: ctx,
		Options: opt,
	})
	if err != nil {
		return err
	}
	return instance.Close()
}
