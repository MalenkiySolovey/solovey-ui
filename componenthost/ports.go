package componenthost

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost"

// These public import paths preserve the existing role-specific integration
// surface while the neutral capability declarations have one source owner.
type OutboundHTTPClientProvider = canonical.OutboundHTTPClientProvider
type OutboundHTTPClientFunc = canonical.OutboundHTTPClientFunc
type AuditRecorder = canonical.AuditRecorder
