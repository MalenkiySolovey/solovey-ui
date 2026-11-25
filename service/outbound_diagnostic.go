package service

import coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"

type outboundDiagnostic = coreruntime.OutboundDiagnostic

// LocalOutboundDiagnostic binds the Core's diagnostic handle to the selected
// local owner. A Runtime replacement can retire it before the old Core stops.
type LocalOutboundDiagnostic struct {
	outboundDiagnostic
	source *coreruntime.Core
}

func LocalOutboundDiagnosticChoices() (outbounds, resolvers []string) {
	return DefaultRuntime().Core().DiagnosticChoices()
}

func OpenLocalOutboundDiagnostic(outbound, resolver string) (LocalOutboundDiagnostic, error) {
	source := DefaultRuntime().Core()
	diagnostic, err := source.OpenOutboundDiagnostic(outbound, resolver)
	if err != nil {
		return LocalOutboundDiagnostic{}, err
	}
	return LocalOutboundDiagnostic{outboundDiagnostic: diagnostic, source: source}, nil
}

func (d LocalOutboundDiagnostic) Current() error {
	if d.source == nil || DefaultRuntime().Core() != d.source {
		return coreruntime.ErrStaleGeneration
	}
	return d.outboundDiagnostic.Current()
}
