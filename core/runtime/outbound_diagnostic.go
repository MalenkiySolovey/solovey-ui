package runtime

import (
	"cmp"
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"

	"github.com/MalenkiySolovey/solovey-backend/security/ssrf"
	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	corelog "github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
)

var (
	ErrDiagnosticOutbound = errors.New("outbound_policy_unverified")
	ErrDiagnosticResolver = errors.New("dns_transport_not_available")
	ErrDiagnosticTarget   = errors.New("target_policy_rejected")
)

// OutboundDiagnostic borrows exact existing owners. It does not perform a
// routing lookup, alter a group, or claim an ingress/client observation.
type OutboundDiagnostic struct {
	core       *Core
	generation string
	outbound   adapter.Outbound
	resolver   adapter.DNSTransport
}

func diagnosticOutboundSupported(outbound adapter.Outbound) bool {
	if outbound == nil || !slices.Contains(outbound.Network(), "tcp") {
		return false
	}
	// These pinned provider classes pass a literal TCP destination through to
	// the direct socket or configured proxy. Others need equivalent evidence.
	switch outbound.Type() {
	case C.TypeDirect, C.TypeHTTP, C.TypeSOCKS:
		return true
	default:
		return false
	}
}

func diagnosticTransport(transport adapter.DNSTransport) string {
	if transport == nil {
		return ""
	}
	switch transport.Type() {
	case "udp":
		return "dns-udp"
	case "tcp":
		return "dns-tcp"
	case "tls":
		return "dot"
	case "https":
		return "doh"
	case "quic":
		return "doq"
	case "h3":
		return "doh3"
	default:
		return ""
	}
}

func (c *Core) DiagnosticChoices() (outbounds, resolvers []string) {
	outbounds, resolvers = []string{}, []string{}
	if c == nil {
		return
	}
	_ = c.withMutation(func(rt coreRuntime) error {
		if rt.generation == "" {
			return ErrCoreUnavailable
		}
		for _, outbound := range rt.outboundManager.Outbounds() {
			if outbound.Tag() != privateAPITag && diagnosticOutboundSupported(outbound) {
				outbounds = append(outbounds, outbound.Tag())
			}
		}
		if owner := service.FromContext[adapter.DNSTransportManager](rt.ctx); owner != nil {
			for _, resolver := range owner.Transports() {
				if resolver.Tag() != "" && diagnosticTransport(resolver) != "" {
					resolvers = append(resolvers, resolver.Tag())
				}
			}
		}
		return nil
	})
	slices.Sort(outbounds)
	slices.Sort(resolvers)
	return
}

func (c *Core) OpenOutboundDiagnostic(tag, resolver string) (OutboundDiagnostic, error) {
	if c == nil || tag == "" || tag == privateAPITag {
		return OutboundDiagnostic{}, ErrOutboundNotFound
	}
	var diagnostic OutboundDiagnostic
	err := c.withMutation(func(rt coreRuntime) error {
		if rt.generation == "" {
			return ErrCoreUnavailable
		}
		selected, exists := rt.outboundManager.Outbound(tag)
		if !exists {
			return ErrOutboundNotFound
		}
		if !diagnosticOutboundSupported(selected) {
			return ErrDiagnosticOutbound
		}
		diagnostic = OutboundDiagnostic{core: c, generation: rt.generation, outbound: selected}
		if resolver == "" {
			return nil
		}
		owner := service.FromContext[adapter.DNSTransportManager](rt.ctx)
		if owner == nil {
			return ErrDiagnosticResolver
		}
		selectedDNS, ok := owner.Transport(resolver)
		if !ok || diagnosticTransport(selectedDNS) == "" {
			return ErrDiagnosticResolver
		}
		diagnostic.resolver = selectedDNS
		return nil
	})
	return diagnostic, err
}

func (d OutboundDiagnostic) Generation() string { return d.generation }

// ConnectOnly reports whether a successful dial is a proxy acknowledgement,
// not direct evidence from a TCP socket opened to the requested destination.
func (d OutboundDiagnostic) ConnectOnly() bool {
	return d.outbound != nil && d.outbound.Type() != C.TypeDirect
}

func (d OutboundDiagnostic) current(rt coreRuntime) bool {
	if rt.generation != d.generation || rt.ctx.Err() != nil {
		return false
	}
	outbound, present := rt.outboundManager.Outbound(d.outbound.Tag())
	if !present || outbound != d.outbound {
		return false
	}
	if d.resolver == nil {
		return true
	}
	owner := service.FromContext[adapter.DNSTransportManager](rt.ctx)
	if owner == nil {
		return false
	}
	resolver, present := owner.Transport(d.resolver.Tag())
	return present && resolver == d.resolver
}

func (d OutboundDiagnostic) Current() error {
	if d.core == nil {
		return ErrCoreUnavailable
	}
	return d.core.withMutation(func(rt coreRuntime) error {
		if !d.current(rt) {
			return ErrStaleGeneration
		}
		return nil
	})
}

func (d OutboundDiagnostic) DialTCP(ctx context.Context, dst netip.AddrPort, permit func(netip.Addr) bool) (net.Conn, error) {
	if d.core == nil || dst.Addr().Zone() != "" || ssrf.IsBlockedAddr(dst.Addr()) ||
		permit == nil || !permit(dst.Addr().Unmap()) || dst.Port() != 80 && dst.Port() != 443 {
		return nil, ErrDiagnosticTarget
	}
	return d.core.dialOutbound(ctx, d.outbound.Tag(), "tcp", M.Socksaddr{Addr: dst.Addr().Unmap(), Port: dst.Port()}, &d)
}

type DiagnosticResolution struct {
	Addresses           []netip.Addr
	ConfiguredTransport string
	RCode               *uint16
}

func (d OutboundDiagnostic) Resolve(ctx context.Context, host string, family, requestedTransport string) (DiagnosticResolution, error) {
	if d.core == nil {
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
	lookup = adapter.WithContext(lookup, &adapter.InboundContext{Outbound: d.outbound.Tag()})
	lookup = corelog.ContextWithNewID(lookup)
	transport := diagnosticTransport(d.resolver)
	if requestedTransport != "dns" && requestedTransport != transport {
		return DiagnosticResolution{}, ErrDiagnosticResolver
	}
	if d.resolver == nil {
		// Use the active lookup policy and its existing caches, including any
		// family policy. Lookup does not attest a wire transport or DNS RCODE.
		addresses, err := owner.Lookup(lookup, host, adapter.DNSQueryOptions{})
		return DiagnosticResolution{Addresses: addresses}, err
	}
	return explicitDiagnosticResolution(lookup, owner, d.resolver, host, family, transport)
}

func explicitDiagnosticResolution(ctx context.Context, owner adapter.DNSRouter, resolver adapter.DNSTransport, host, family, transport string) (DiagnosticResolution, error) {
	// The DNS owner exposes the configured transport, not the wire exchange.
	// Both providers can retry UDP over TCP; policy can also synthesize replies.
	result := DiagnosticResolution{ConfiguredTransport: transport}
	classes := []uint16{dns.TypeA, dns.TypeAAAA}
	if family == "ipv4" {
		classes = classes[:1]
	} else if family == "ipv6" {
		classes = classes[1:]
	}
	var rcode uint16
	consistent := true
	for i, queryType := range classes {
		message := new(dns.Msg).SetQuestion(dns.Fqdn(host), queryType)
		response, err := owner.Exchange(ctx, message, adapter.DNSQueryOptions{Transport: resolver, DisableCache: true, DisableOptimisticCache: true})
		if err != nil {
			return DiagnosticResolution{}, err
		}
		if response == nil || !response.Response || response.Truncated || response.Rcode < 0 || response.Rcode > 4095 {
			return DiagnosticResolution{}, errors.New("invalid_dns_response")
		}
		if i == 0 {
			rcode = uint16(response.Rcode)
		} else {
			consistent = rcode == uint16(response.Rcode)
		}
		for _, answer := range response.Answer {
			switch record := answer.(type) {
			case *dns.A:
				result.Addresses = append(result.Addresses, M.AddrFromIP(record.A))
			case *dns.AAAA:
				result.Addresses = append(result.Addresses, M.AddrFromIP(record.AAAA))
			}
		}
	}
	// Do not collapse different A/AAAA outcomes into a single wire RCODE.
	if consistent {
		result.RCode = &rcode
	}
	return result, nil
}
