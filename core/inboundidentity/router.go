package inboundidentity

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	N "github.com/sagernet/sing/common/network"
)

type boundRouter struct {
	adapter.Router
	epoch *Epoch
}

func (r *boundRouter) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	ctx, err := r.epoch.bind(ctx, metadata)
	if err != nil {
		return err
	}
	return r.Router.RouteConnection(ctx, conn, metadata)
}

func (r *boundRouter) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	ctx, err := r.epoch.bind(ctx, metadata)
	if err != nil {
		return err
	}
	return r.Router.RoutePacketConnection(ctx, conn, metadata)
}

func (r *boundRouter) RouteConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	ctx, err := r.epoch.bind(ctx, metadata)
	if err != nil {
		_ = N.CloseOnHandshakeFailure(conn, onClose, err)
		return
	}
	r.Router.RouteConnectionEx(ctx, conn, metadata, onClose)
}

func (r *boundRouter) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	ctx, err := r.epoch.bind(ctx, metadata)
	if err != nil {
		_ = N.CloseOnHandshakeFailure(conn, onClose, err)
		return
	}
	r.Router.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}
