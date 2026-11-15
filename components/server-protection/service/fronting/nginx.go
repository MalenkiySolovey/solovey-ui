package fronting

import "context"

const NginxCapabilityRevision = "fronting.nginx.v1"

// NginxCapabilitySource supplies a read-only report from the Backend inspector.
type NginxCapabilitySource interface {
	Capability(context.Context) CapabilityReport
}

type NginxAdapter struct {
	Workflow   *Workflow
	Inspection NginxCapabilitySource
}

func NewNginxAdapter() *NginxAdapter { return &NginxAdapter{} }
func NewManagedNginxAdapter(workflow *Workflow) *NginxAdapter {
	return &NginxAdapter{Workflow: workflow}
}
func NewInspectedNginxAdapter(owner NginxCapabilitySource) *NginxAdapter {
	return &NginxAdapter{Inspection: owner}
}
func (a *NginxAdapter) ID() string   { return "nginx" }
func (a *NginxAdapter) Kind() string { return "stream" }
func (a *NginxAdapter) Capability(ctx context.Context) CapabilityReport {
	if a.Workflow != nil {
		return a.Workflow.Capability(ctx)
	}
	if a.Inspection != nil {
		return a.Inspection.Capability(ctx)
	}
	reason := "nginx binary inspection capability is unavailable"
	return CapabilityReport{AdapterID: "nginx", AdapterKind: "stream", CapabilityRevision: NginxCapabilityRevision, State: StateUnsupported, Reason: reason,
		ManagedRoot: AvailabilityUnsupported, ControlledInclude: AvailabilityUnsupported, Validate: AvailabilityUnsupported, Activate: AvailabilityUnsupported, Reload: AvailabilityUnsupported,
		SNI: AvailabilityUnsupported, ALPN: AvailabilityUnsupported, ProxyProtocol: AvailabilityUnsupported,
		Warnings: []string{reason, "preview-only: nginx validation, activation, reload, Sync, and Apply are unavailable"}, Diagnostics: []string{}}
}
