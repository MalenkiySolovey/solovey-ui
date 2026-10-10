//go:build with_quic

package registry

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/hysteria"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/tuic"
	qtls "github.com/sagernet/sing-quic"
)

func QUICParentControlCompiled() bool { return true }

func QUICAdmission(ctx context.Context, accept func()) bool { return qtls.ParentAdmission(ctx, accept) }

type quicInboundControl struct {
	owner    *qtls.ParentOwner[int]
	names    []string
	hooks    int
	once     sync.Once
	closeErr error
}

func newQUICControl(ctx context.Context, names []string) (context.Context, *quicInboundControl) {
	control := &quicInboundControl{names: names}
	return qtls.WithParentControl(ctx, func(owner *qtls.ParentOwner[int]) {
		control.hooks++
		control.owner = owner
	}), control
}

func (q *quicInboundControl) finish(candidate adapter.Inbound, err error) (adapter.Inbound, error) {
	if err == nil && (q.hooks != 1 || q.owner == nil) {
		err = errors.New("QUIC constructor lifecycle hook unavailable")
	}
	if err != nil {
		if candidate != nil {
			err = errors.Join(err, q.close(candidate.Close))
		} else if q.owner != nil {
			err = errors.Join(err, q.owner.Close())
		}
		return nil, err
	}
	return candidate, nil
}

func (q *quicInboundControl) close(closeInbound func() error) error {
	q.once.Do(func() {
		var parentErr error
		if q.owner != nil {
			parentErr = q.owner.Close()
		}
		// The official inbound closes UDP first. Drain its exposed service owner
		// before delegation; the service's second Close is idempotent.
		q.closeErr = errors.Join(parentErr, closeInbound())
	})
	return q.closeErr
}

func (q *quicInboundControl) QUICParents(limit int) ([]QUICParent, int) {
	parents, total := q.owner.Snapshot(limit)
	result := make([]QUICParent, 0, len(parents))
	for _, parent := range parents {
		if parent.User >= 0 && parent.User < len(q.names) {
			result = append(result, QUICParent{ID: strconv.FormatUint(parent.ID, 10), Principal: q.names[parent.User], CreatedAt: parent.CreatedAt.UnixMilli()})
		}
	}
	return result, total
}

func (q *quicInboundControl) CloseQUICParent(ctx context.Context, id, principal string) error {
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil || value == 0 {
		return qtls.ErrParentUnavailable
	}
	parent, found := q.owner.Get(value)
	if !found || parent.User < 0 || parent.User >= len(q.names) || principal == "" || q.names[parent.User] != principal {
		return qtls.ErrParentUnavailable
	}
	return q.owner.CloseParent(ctx, value, parent.User)
}

func (q *quicInboundControl) QUICParent(id string) (QUICParent, bool) {
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return QUICParent{}, false
	}
	parent, found := q.owner.Get(value)
	if !found || parent.User < 0 || parent.User >= len(q.names) {
		return QUICParent{}, false
	}
	return QUICParent{ID: strconv.FormatUint(parent.ID, 10), Principal: q.names[parent.User], CreatedAt: parent.CreatedAt.UnixMilli()}, true
}

var _ adapter.InterfaceUpdateListener = (*hysteria2Controlled)(nil)

// Typed embedding preserves protocol-specific optional interfaces, including
// Hysteria2.InterfaceUpdated. A generic adapter.Inbound wrapper would erase them.
type hysteriaControlled struct {
	*hysteria.Inbound
	*quicInboundControl
}

func (h *hysteriaControlled) Close() error { return h.close(h.Inbound.Close) }

type hysteria2Controlled struct {
	*hysteria2.Inbound
	*quicInboundControl
}

func (h *hysteria2Controlled) Close() error { return h.close(h.Inbound.Close) }

type tuicControlled struct {
	*tuic.Inbound
	*quicInboundControl
}

func (h *tuicControlled) Close() error { return h.close(h.Inbound.Close) }

func registerHysteriaInbound(r *inbound.Registry) {
	inbound.Register[option.HysteriaInboundOptions](r, C.TypeHysteria, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.HysteriaInboundOptions) (adapter.Inbound, error) {
		names := make([]string, len(options.Users))
		for index, user := range options.Users {
			names[index] = user.Name
		}
		ctx, control := newQUICControl(ctx, names)
		candidate, err := control.finish(hysteria.NewInbound(ctx, router, logger, tag, options))
		if err != nil {
			return nil, err
		}
		return &hysteriaControlled{candidate.(*hysteria.Inbound), control}, nil
	})
}

func registerHysteria2Inbound(r *inbound.Registry) {
	inbound.Register[option.Hysteria2InboundOptions](r, C.TypeHysteria2, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.Hysteria2InboundOptions) (adapter.Inbound, error) {
		names := make([]string, len(options.Users))
		for index, user := range options.Users {
			names[index] = user.Name
		}
		ctx, control := newQUICControl(ctx, names)
		candidate, err := control.finish(hysteria2.NewInbound(ctx, router, logger, tag, options))
		if err != nil {
			return nil, err
		}
		return &hysteria2Controlled{candidate.(*hysteria2.Inbound), control}, nil
	})
}

func registerTUICInbound(r *inbound.Registry) {
	inbound.Register[option.TUICInboundOptions](r, C.TypeTUIC, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.TUICInboundOptions) (adapter.Inbound, error) {
		names := make([]string, len(options.Users))
		for index, user := range options.Users {
			names[index] = user.Name
		}
		ctx, control := newQUICControl(ctx, names)
		candidate, err := control.finish(tuic.NewInbound(ctx, router, logger, tag, options))
		if err != nil {
			return nil, err
		}
		return &tuicControlled{candidate.(*tuic.Inbound), control}, nil
	})
}
