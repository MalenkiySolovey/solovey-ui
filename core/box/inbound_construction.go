package box

import (
	"context"
	"errors"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

// The pinned inbound manager does not close an unpublished hot candidate when
// Start or replacement fails. Capture its construction without wrapping the
// inbound itself, preserving protocol-specific optional interfaces.
type inboundConstructionKey struct{}
type inboundConstruction struct {
	tag       string
	candidate adapter.Inbound
	once      sync.Once
	err       error
}

func BeginInboundConstruction(ctx context.Context, tag string) (context.Context, func(error) error) {
	lease := &inboundConstruction{tag: tag}
	return context.WithValue(ctx, inboundConstructionKey{}, lease), func(err error) error {
		lease.once.Do(func() {
			lease.err = err
			if err != nil && lease.candidate != nil {
				lease.err = errors.Join(err, lease.candidate.Close())
			}
		})
		return lease.err
	}
}

type constructionRegistry struct{ adapter.InboundRegistry }

func (r constructionRegistry) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag, kind string, options any) (adapter.Inbound, error) {
	candidate, err := r.InboundRegistry.Create(ctx, router, logger, tag, kind, options)
	if lease, _ := ctx.Value(inboundConstructionKey{}).(*inboundConstruction); lease != nil && lease.tag == tag {
		lease.candidate = candidate
	}
	return candidate, err
}
