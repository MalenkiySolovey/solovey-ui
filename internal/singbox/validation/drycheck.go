package validation

import (
	"context"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
)

type DryChecker struct{}

func NewDryChecker() DryChecker {
	return DryChecker{}
}

func (DryChecker) ValidateConfig(sbConfig []byte) error {
	ctx, opt, err := decodeConfig(sbConfig)
	if err != nil {
		return err
	}
	instance, err := corebox.NewBox(corebox.Options{Context: ctx, Options: opt})
	if err != nil {
		return err
	}
	return instance.Close()
}

// ValidateConfigShape uses the same owner checks and pinned decoder without
// constructing transports or reading local rule-set/certificate files. Draft
// previews use it before the asset owner prepares the complete apply candidate.
func (DryChecker) ValidateConfigShape(sbConfig []byte) error {
	_, _, err := decodeConfig(sbConfig)
	return err
}

func decodeConfig(sbConfig []byte) (context.Context, option.Options, error) {
	ctx := registry.Context(context.Background())
	var opt option.Options
	if err := diagnostics.FirstError(entitytls.CertificateConfigFindings(sbConfig)); err != nil {
		return ctx, opt, err
	}
	if err := diagnostics.FirstError(entitytls.TLSConfigFindings(sbConfig)); err != nil {
		return ctx, opt, err
	}
	if err := diagnostics.FirstError(entityprotocol.ConfigFindings(sbConfig)); err != nil {
		return ctx, opt, err
	}
	if _, err := ValidateRuleConditions(sbConfig); err != nil {
		return ctx, opt, err
	}
	if _, err := singboxconfig.ValidateDNSConfig(sbConfig); err != nil {
		return ctx, opt, err
	}
	if _, err := singboxconfig.ValidateHTTPConfig(sbConfig); err != nil {
		return ctx, opt, err
	}
	if err := diagnostics.FirstError(entityinbounds.TUNDNSFindings(sbConfig)); err != nil {
		return ctx, opt, err
	}
	if err := opt.UnmarshalJSONContext(ctx, sbConfig); err != nil {
		return ctx, opt, err
	}
	return ctx, opt, nil
}
