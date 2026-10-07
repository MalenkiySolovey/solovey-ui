package validation

import (
	"context"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
)

type DryChecker struct{}

func NewDryChecker() DryChecker {
	return DryChecker{}
}

func (DryChecker) ValidateConfig(sbConfig []byte) error {
	if _, err := ValidateRuleConditions(sbConfig); err != nil {
		return err
	}
	if _, err := singboxconfig.ValidateDNSConfig(sbConfig); err != nil {
		return err
	}
	if _, err := singboxconfig.ValidateHTTPConfig(sbConfig); err != nil {
		return err
	}
	if err := diagnostics.FirstError(entityinbounds.TUNDNSFindings(sbConfig)); err != nil {
		return err
	}
	var opt option.Options
	ctx := context.Background()
	ctx = registry.Context(ctx)
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
