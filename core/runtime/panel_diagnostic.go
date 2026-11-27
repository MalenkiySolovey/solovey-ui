package runtime

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/MalenkiySolovey/solovey-backend/security/ssrf"
	"github.com/sagernet/sing-box/adapter"
	corelog "github.com/sagernet/sing-box/log"
	R "github.com/sagernet/sing-box/route/rule"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/uot"
	"github.com/sagernet/sing/service"
)

var (
	ErrDiagnosticIngress    = errors.New("panel_ingress_not_qualified")
	ErrDiagnosticPanelRoute = errors.New("panel_final_egress_unverified")
	ErrDiagnosticPanelDNS   = errors.New("panel_final_address_unverified")
	ErrDiagnosticRouteDeny  = errors.New("panel_route_denied")
)

// PanelDiagnostic borrows one active ingress in one Core generation. It
// represents a local anonymous proxy client, not an arbitrary remote user.
type PanelDiagnostic struct {
	core       *Core
	ingress    adapter.TCPInjectableInbound
	generation string
}

func (c *Core) PanelDiagnosticChoices() []string {
	choices := []string{}
	if c == nil {
		return choices
	}
	_ = c.withMutation(func(rt coreRuntime) error {
		if rt.generation == "" {
			return ErrCoreUnavailable
		}
		for _, inbound := range rt.inboundManager.Inbounds() {
			if c.panelDiagnosticIngress(inbound) != nil {
				choices = append(choices, inbound.Tag())
			}
		}
		return nil
	})
	slices.Sort(choices)
	return choices
}

func (c *Core) panelDiagnosticIngress(inbound adapter.Inbound) adapter.TCPInjectableInbound {
	stream, ok := inbound.(adapter.TCPInjectableInbound)
	if !ok || stream.Tag() == "" || stream.Tag() == privateAPITag {
		return nil
	}
	c.access.RLock()
	defer c.access.RUnlock()
	record := c.effectiveInbounds[stream.Tag()]
	if !record.panelStream || record.Type != stream.Type() || record.ownerTag != "" || record.ManagerGeneration != c.managerGeneration {
		return nil
	}
	return stream
}

func (c *Core) OpenPanelDiagnostic(tag string) (PanelDiagnostic, error) {
	if c == nil || tag == "" {
		return PanelDiagnostic{}, ErrDiagnosticIngress
	}
	var diagnostic PanelDiagnostic
	err := c.withMutation(func(rt coreRuntime) error {
		if rt.generation == "" {
			return ErrCoreUnavailable
		}
		active, _ := rt.inboundManager.Get(tag)
		ingress := c.panelDiagnosticIngress(active)
		if ingress == nil {
			return ErrDiagnosticIngress
		}
		diagnostic = PanelDiagnostic{core: c, ingress: ingress, generation: rt.generation}
		return nil
	})
	return diagnostic, err
}

func (d PanelDiagnostic) Generation() string { return d.generation }

func (d PanelDiagnostic) current(rt coreRuntime) bool {
	return rt.generation == d.generation && rt.ctx.Err() == nil && d.sameIngress(rt.inboundManager)
}

func (d PanelDiagnostic) sameIngress(manager adapter.InboundManager) bool {
	active, exists := manager.Get(d.ingress.Tag())
	return exists && active == d.ingress
}

func (d PanelDiagnostic) Current() error {
	if d.core == nil || d.ingress == nil {
		return ErrCoreUnavailable
	}
	return d.core.withMutation(func(rt coreRuntime) error {
		if !d.current(rt) {
			return ErrStaleGeneration
		}
		return nil
	})
}

func (d PanelDiagnostic) metadata() adapter.InboundContext {
	return adapter.InboundContext{
		Inbound: d.ingress.Tag(), InboundType: d.ingress.Type(), Network: N.NetworkTCP,
		Source: M.Socksaddr{Addr: netip.AddrFrom4([4]byte{127, 0, 0, 1})},
	}
}

func (d PanelDiagnostic) Resolve(ctx context.Context, host string, requestedTransport string) (DiagnosticResolution, error) {
	if requestedTransport != "dns" {
		return DiagnosticResolution{}, ErrDiagnosticResolver
	}
	if d.core == nil || d.ingress == nil {
		return DiagnosticResolution{}, ErrCoreUnavailable
	}
	var owner adapter.DNSRouter
	var generation context.Context
	err := d.core.withMutation(func(rt coreRuntime) error {
		if !d.current(rt) {
			return ErrStaleGeneration
		}
		owner, generation = service.FromContext[adapter.DNSRouter](rt.ctx), rt.ctx
		if owner == nil {
			return ErrDiagnosticResolver
		}
		return nil
	})
	if err != nil {
		return DiagnosticResolution{}, err
	}
	lookup, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(generation, cancel)
	defer stop()
	if generation.Err() != nil || ctx.Err() != nil {
		return DiagnosticResolution{}, cmp.Or(ctx.Err(), ErrStaleGeneration)
	}
	metadata := d.metadata()
	metadata.Destination = M.Socksaddr{Fqdn: host}
	lookup = corelog.ContextWithNewID(adapter.WithContext(lookup, &metadata))
	addresses, err := owner.Lookup(lookup, host, adapter.DNSQueryOptions{})
	if generation.Err() != nil {
		return DiagnosticResolution{}, ErrStaleGeneration
	}
	return DiagnosticResolution{Addresses: addresses}, err
}

type PanelRouteObservation struct {
	Egress string
	Err    error
}

func (d PanelDiagnostic) DialStream(ctx context.Context, host string, dst netip.AddrPort, permit func(netip.Addr) bool) (net.Conn, func() PanelRouteObservation, error) {
	addr := dst.Addr().Unmap()
	if dst.Addr().Zone() != "" || ssrf.IsBlockedAddr(addr) || (dst.Port() != 80 && dst.Port() != 443) {
		return nil, nil, ErrDiagnosticTarget
	}
	return d.dialStream(ctx, host, netip.AddrPortFrom(addr, dst.Port()), permit)
}

func (d PanelDiagnostic) dialStream(ctx context.Context, host string, dst netip.AddrPort, permit func(netip.Addr) bool) (net.Conn, func() PanelRouteObservation, error) {
	if !panelAuthority(host, dst.Addr()) || permit == nil || !permit(dst.Addr()) {
		return nil, nil, ErrDiagnosticTarget
	}
	if d.core == nil || d.ingress == nil {
		return nil, nil, ErrCoreUnavailable
	}
	var rt coreRuntime
	err := d.core.withMutation(func(active coreRuntime) error {
		if !d.current(active) {
			return ErrStaleGeneration
		}
		rt = active
		d.core.outboundDials.Add(1)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(rt.ctx, cancel)
	client, native := net.Pipe()
	stopClose := context.AfterFunc(streamCtx, func() { _ = client.Close(); _ = native.Close() })
	stream := &panelProbeStream{Conn: client, reader: bufio.NewReader(client), release: func() {
		stop()
		stopClose()
		cancel()
		_ = native.Close()
	}}
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetDeadline(deadline)
		_ = native.SetDeadline(deadline)
	}
	var decision atomic.Pointer[PanelRouteObservation]
	observe := func() PanelRouteObservation {
		if rt.ctx.Err() != nil || !d.sameIngress(rt.inboundManager) {
			return PanelRouteObservation{Err: ErrStaleGeneration}
		}
		if current := decision.Load(); current != nil {
			return *current
		}
		return PanelRouteObservation{}
	}
	guard := func(final adapter.InboundContext, egress adapter.Outbound) error {
		result := PanelRouteObservation{Err: panelDispatch(final, egress, d.ingress.Tag(), host, dst, permit)}
		if rt.ctx.Err() != nil || !d.sameIngress(rt.inboundManager) {
			result.Err = ErrStaleGeneration
		}
		if result.Err == nil {
			result.Egress = egress.Tag()
		}
		decision.Store(&result)
		return result.Err
	}
	streamCtx = adapter.ContextWithStreamRouteGuard(corelog.ContextWithNewID(streamCtx), guard)
	go func() {
		defer d.core.outboundDials.Done()
		d.ingress.NewConnection(streamCtx, native, d.metadata(), N.OnceClose(func(err error) {
			if errors.Is(err, adapter.ErrUnguardedStreamAction) || R.IsRejected(err) {
				decision.CompareAndSwap(nil, &PanelRouteObservation{Err: ErrDiagnosticRouteDeny})
			}
			// The mixed listener also calls this after handing off successfully.
			// A nil handoff error does not mean the routed stream is finished.
			if err != nil {
				_ = native.Close()
			}
		}))
	}()
	authority := net.JoinHostPort(host, strconv.Itoa(int(dst.Port())))
	if _, err = fmt.Fprintf(stream, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", authority, authority); err == nil {
		err = readPanelConnectResponse(stream.reader)
	}
	if err != nil {
		_ = stream.Close()
		return nil, observe, err
	}
	// This only establishes the ingress tunnel. Native routing may wait for
	// the probe's TLS ClientHello or HTTP request before dispatching anything.
	return stream, observe, nil
}

func readPanelConnectResponse(reader *bufio.Reader) error {
	// Use header-only parsing: after CONNECT 200 the buffered bytes belong
	// to the tunnel, not to an HTTP body. A refusal closes the stream.
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodHead})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrDiagnosticIngress
	}
	return nil
}

func panelAuthority(host string, address netip.Addr) bool {
	if host == uot.MagicAddress || host == uot.LegacyMagicAddress {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == "" && ip.Unmap() == address
	}
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" ||
			label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

func panelDispatch(final adapter.InboundContext, egress adapter.Outbound, ingress, host string, dst netip.AddrPort, permit func(netip.Addr) bool) error {
	if final.Inbound != ingress || final.User != "" || final.Network != N.NetworkTCP || diagnosticTCPMode(egress) == DiagnosticTCPUnavailable {
		return ErrDiagnosticPanelRoute
	}
	// These reviewed leaf dialers use the admitted literal via the native
	// ConnectionManager. A group or alternate connection handler is not proof.
	if _, customHandler := egress.(adapter.ConnectionHandler); customHandler || egress.Tag() == privateAPITag {
		return ErrDiagnosticPanelRoute
	}
	if final.Destination.Port != dst.Port() || !permit(dst.Addr()) {
		return ErrDiagnosticTarget
	}
	if final.Destination.IsIP() {
		if final.Destination.Addr.Unmap() != dst.Addr() || len(final.DestinationAddresses) != 0 {
			return ErrDiagnosticTarget
		}
		return nil
	}
	if final.Destination.Fqdn != host {
		return ErrDiagnosticTarget
	}
	if len(final.DestinationAddresses) != 1 {
		return ErrDiagnosticPanelDNS
	}
	if final.DestinationAddresses[0].Unmap() != dst.Addr() {
		return ErrDiagnosticTarget
	}
	return nil
}

type panelProbeStream struct {
	net.Conn
	reader  *bufio.Reader
	release func()
}

func (c *panelProbeStream) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *panelProbeStream) Close() error {
	c.release()
	return c.Conn.Close()
}
