//go:build !with_quic

package registry

import (
	"context"

	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/protocol/hysteria"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/tuic"
)

func QUICParentControlCompiled() bool { return false }
func QUICAdmission(ctx context.Context, accept func()) bool {
	if ctx.Err() != nil {
		return false
	}
	accept()
	return true
}

func registerHysteriaInbound(r *inbound.Registry)  { hysteria.RegisterInbound(r) }
func registerHysteria2Inbound(r *inbound.Registry) { hysteria2.RegisterInbound(r) }
func registerTUICInbound(r *inbound.Registry)      { tuic.RegisterInbound(r) }
