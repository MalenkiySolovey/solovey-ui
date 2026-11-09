package manifest

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"

const (
	HostAPIV1              = canonical.HostAPIV1
	PortScheduler          = canonical.PortScheduler
	PortOutboundHTTP       = canonical.PortOutboundHTTP
	PortAuditRecord        = canonical.PortAuditRecord
	PortHTTPRequest        = canonical.PortHTTPRequest
	PortSettingsDrop       = canonical.PortSettingsDrop
	PortObservationSample  = canonical.PortObservationSample
	PortObservationHistory = canonical.PortObservationHistory
	PortAuditHistory       = canonical.PortAuditHistory
)

type HostAPI = canonical.HostAPI
