//go:build !minimal

package blockcheck

import (
	"context"
	"errors"
	"net"
	"net/netip"

	"github.com/MalenkiySolovey/solovey-backend/security/ssrf"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type localProvider struct{}

type pathOptions struct {
	Outbounds   []string                           `json:"outbounds"`
	Resolvers   []string                           `json:"resolvers"`
	Unavailable map[feature.Mode]feature.PathError `json:"unavailable"`
	History     bool                               `json:"history"`
}

func (localProvider) options() pathOptions {
	outbounds, resolvers := service.LocalOutboundDiagnosticChoices()
	result := pathOptions{Outbounds: outbounds, Resolvers: resolvers, History: true, Unavailable: map[feature.Mode]feature.PathError{
		feature.Panel: feature.ErrPanelIngressUnqualified, feature.HostBaseline: feature.ErrBaselineUnobservable,
	}}
	if len(outbounds) == 0 {
		result.Unavailable[feature.Outbound] = feature.ErrOutboundUnavailable
	}
	return result
}

func (localProvider) Open(_ context.Context, host hostref.Ref, selection feature.PathSelection) (feature.Path, error) {
	if !host.IsLocal() {
		return feature.Path{}, feature.ErrInactive
	}
	switch selection.Mode {
	case feature.Panel:
		return feature.Path{}, feature.ErrPanelIngressUnqualified
	case feature.HostBaseline:
		return feature.Path{}, feature.ErrBaselineUnobservable
	case feature.Outbound:
	default:
		return feature.Path{}, feature.ErrInvalidRun
	}
	diagnostic, err := service.OpenLocalOutboundDiagnostic(selection.Ref, selection.Resolver)
	if err != nil {
		return feature.Path{}, pathError(err)
	}
	dnsContext := "active-core-lookup-no-ingress"
	if selection.Resolver != "" {
		dnsContext = "explicit-core-dns-server"
	}
	scope := "outbound-pinned-ip"
	tcpMode := diagnostic.TCPMode()
	switch tcpMode {
	case coreruntime.DiagnosticTCPProxyConnect:
		scope = "outbound-proxy-connect"
	case coreruntime.DiagnosticTCPProxyStream:
		scope = "outbound-proxy-stream"
	}
	path := feature.Path{
		Evidence: feature.PathEvidence{Host: host, Mode: feature.Outbound, Generation: diagnostic.Generation(),
			Scope: scope, Context: selection.Ref},
		Current: func(ctx context.Context) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if diagnostic.Current() != nil {
				return feature.ErrGenerationChanged
			}
			return nil
		},
		Resolve: func(ctx context.Context, host string, family feature.Family, probe feature.Probe) (feature.Resolution, error) {
			value, err := diagnostic.Resolve(ctx, host, string(family), string(probe))
			return feature.Resolution{Addresses: value.Addresses, RCode: value.RCode, Context: dnsContext, ConfiguredTransport: value.ConfiguredTransport}, pathError(err)
		},
		DialTCP: func(ctx context.Context, destination netip.AddrPort) (net.Conn, feature.SocketEvidence, error) {
			conn, err := diagnostic.DialTCP(ctx, destination, permitTarget)
			var observation feature.SocketEvidence
			if err == nil && conn != nil {
				observation.ProxyAccepted = tcpMode == coreruntime.DiagnosticTCPProxyConnect
				observation.ProxyTransportOnly = tcpMode == coreruntime.DiagnosticTCPProxyStream
			}
			return conn, observation, pathError(err)
		},
	}
	if diagnostic.SupportsUDP() {
		path.DialUDP = func(ctx context.Context, destination netip.AddrPort) (net.Conn, feature.SocketEvidence, error) {
			conn, err := diagnostic.DialUDP(ctx, destination, permitTarget)
			return conn, feature.SocketEvidence{}, pathError(err)
		}
	}
	return path, nil
}

func pathError(err error) error {
	switch {
	case errors.Is(err, coreruntime.ErrDiagnosticResolver):
		return feature.ErrResolverUnsupported
	case errors.Is(err, coreruntime.ErrDiagnosticOutbound):
		return feature.PathError("outbound_policy_unverified")
	case errors.Is(err, coreruntime.ErrDiagnosticDatagram):
		return feature.ErrNoProbeCapability
	case errors.Is(err, coreruntime.ErrOutboundNotFound):
		return feature.ErrOutboundUnavailable
	case errors.Is(err, coreruntime.ErrStaleGeneration), errors.Is(err, coreruntime.ErrCoreUnavailable):
		return feature.ErrGenerationChanged
	case errors.Is(err, coreruntime.ErrDiagnosticTarget):
		return feature.ErrTargetRejected
	default:
		return err
	}
}

// Also exclude this host's public listeners. The final socket operation invokes
// the same check afresh rather than trusting the previous DNS admission.
func permitTarget(address netip.Addr) bool {
	if address.Zone() != "" || ssrf.IsBlockedAddr(address) {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, local := range addrs {
		if prefix, err := netip.ParsePrefix(local.String()); err == nil && prefix.Addr().Unmap() == address.Unmap() {
			return false
		}
	}
	return true
}
