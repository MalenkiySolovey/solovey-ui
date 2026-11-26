package service

import (
	"context"
	"net"
	"net/netip"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
)

type LocalPanelDiagnostic struct {
	diagnostic coreruntime.PanelDiagnostic
	source     *coreruntime.Core
}

func LocalPanelDiagnosticChoices() []string {
	return DefaultRuntime().Core().PanelDiagnosticChoices()
}

func OpenLocalPanelDiagnostic(ingress string) (LocalPanelDiagnostic, error) {
	source := DefaultRuntime().Core()
	diagnostic, err := source.OpenPanelDiagnostic(ingress)
	if err != nil {
		return LocalPanelDiagnostic{}, err
	}
	return LocalPanelDiagnostic{diagnostic: diagnostic, source: source}, nil
}

func (d LocalPanelDiagnostic) Generation() string { return d.diagnostic.Generation() }

func (d LocalPanelDiagnostic) Current() error {
	if d.source == nil || d.source != DefaultRuntime().Core() {
		return coreruntime.ErrStaleGeneration
	}
	return d.diagnostic.Current()
}

func (d LocalPanelDiagnostic) Resolve(ctx context.Context, host, transport string) (coreruntime.DiagnosticResolution, error) {
	if err := d.Current(); err != nil {
		return coreruntime.DiagnosticResolution{}, err
	}
	result, err := d.diagnostic.Resolve(ctx, host, transport)
	if stale := d.Current(); stale != nil {
		return coreruntime.DiagnosticResolution{}, stale
	}
	return result, err
}

func (d LocalPanelDiagnostic) DialStream(ctx context.Context, host string, target netip.AddrPort, permit func(netip.Addr) bool) (net.Conn, func() coreruntime.PanelRouteObservation, error) {
	if err := d.Current(); err != nil {
		return nil, nil, err
	}
	conn, observe, err := d.diagnostic.DialStream(ctx, host, target, func(address netip.Addr) bool {
		return permit != nil && d.source == DefaultRuntime().Core() && permit(address)
	})
	if observe == nil {
		return conn, nil, err
	}
	return conn, func() coreruntime.PanelRouteObservation {
		if d.source != DefaultRuntime().Core() {
			return coreruntime.PanelRouteObservation{Err: coreruntime.ErrStaleGeneration}
		}
		return observe()
	}, err
}
