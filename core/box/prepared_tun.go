package box

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/service"
)

var errPreparedTUN = errors.New("an admitted Linux TUN descriptor is required")

// PreparedTUN is a one-shot, out-of-band input for a single Core startup.
// Receive transfers ownership of a broker-prepared device only when the exact
// inbound opens it. None of this authority is accessible via provider JSON.
type PreparedTUN struct {
	tag, name string
	once      sync.Once
	receive   func(context.Context) (*os.File, error)
}

func NewPreparedTUN(tag, name string, receive func(context.Context) (*os.File, error)) *PreparedTUN {
	return &PreparedTUN{tag: tag, name: name, receive: receive}
}

func (p *PreparedTUN) validate(inbounds []option.Inbound) error {
	if p == nil {
		return nil
	}
	if runtime.GOOS != "linux" || p.name == "" || p.tag == "" || p.receive == nil {
		return errPreparedTUN
	}
	var matched bool
	for _, inbound := range inbounds {
		if inbound.Type != C.TypeTun && inbound.Tag != p.tag {
			continue
		}
		options, ok := inbound.Options.(*option.TunInboundOptions)
		if matched || inbound.Type != C.TypeTun || inbound.Tag != p.tag || !ok || options == nil ||
			options.InterfaceName != p.name || options.AutoRoute || options.AutoRedirect || options.NetNs != "" ||
			options.DNSMode != tun.DNSModeDisabled || options.Platform != nil {
			return errPreparedTUN
		}
		matched = true
	}
	if !matched {
		return errPreparedTUN
	}
	return nil
}

func (p *PreparedTUN) createInbound(ctx context.Context, tag string, create func(context.Context) error) error {
	if p == nil || p.tag != tag {
		return create(ctx)
	}
	inboundCtx := service.ContextWith[adapter.PlatformInterface](service.ExtendContext(ctx), &preparedTUNPlatform{ctx: ctx, input: p})
	// The pinned TUN constructor retains the hook itself. Remove it from
	// the private registry before any flow context can reach another owner.
	defer service.MustRegister[adapter.PlatformInterface](inboundCtx, nil)
	return create(inboundCtx)
}

// Only the pinned TUN constructor receives this platform service. Both pinned
// TUN implementations call just these methods on its external-device path.
type preparedTUNPlatform struct {
	adapter.PlatformInterface
	ctx   context.Context
	input *PreparedTUN
}

func (*preparedTUNPlatform) UnderNetworkExtension() bool                            { return false }
func (*preparedTUNPlatform) UsePlatformInterface() bool                             { return true }
func (*preparedTUNPlatform) ProcessPlatformOptions(option.TunPlatformOptions) error { return nil }

func (p *preparedTUNPlatform) OpenInterface(options *tun.Options, _ option.TunPlatformOptions) (tun.Tun, error) {
	if options.Name != p.input.name || options.NetNs != "" || options.AutoRoute || options.AutoRedirectMarkMode ||
		options.DNSMode != tun.DNSModeDisabled || options.GSO || options.FileDescriptor != 0 {
		return nil, errPreparedTUN
	}
	var device tun.Tun
	err := errPreparedTUN
	p.input.once.Do(func() {
		var file *os.File
		file, err = p.input.receive(p.ctx)
		if err != nil || file == nil {
			if file != nil {
				_ = file.Close()
			}
			err = errors.Join(errPreparedTUN, err)
			return
		}
		defer file.Close()
		device, err = usePreparedTUN(file, *options)
	})
	return device, err
}
