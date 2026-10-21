package manifest

import "fmt"

const HostAPIV1 = "solovey.component-host/v1"

const (
	PortScheduler          = "scheduler"
	PortOutboundHTTP       = "outbound.http"
	PortAuditRecord        = "audit.record"
	PortHTTPRequest        = "http.request"
	PortSettingsDrop       = "settings.drop-owned"
	PortObservationSample  = "observability.sample"
	PortObservationHistory = "observability.history"
	PortAuditHistory       = "audit.history"
)

// HostAPI declares the existing owner ports required by each entry point.
// Omission preserves the legacy in-process contract for unmigrated Components.
type HostAPI struct {
	Version   string   `json:"version"`
	Lifecycle []string `json:"lifecycle,omitempty"`
	API       []string `json:"api,omitempty"`
	DropData  []string `json:"dropData,omitempty"`
}

func (h *HostAPI) Clone() *HostAPI {
	if h == nil {
		return nil
	}
	copy := *h
	copy.Lifecycle = append([]string(nil), h.Lifecycle...)
	copy.API = append([]string(nil), h.API...)
	copy.DropData = append([]string(nil), h.DropData...)
	return &copy
}

func (h *HostAPI) Validate() error {
	if h == nil {
		return nil
	}
	if h.Version != HostAPIV1 {
		return fmt.Errorf("unsupported component Host API %q", h.Version)
	}
	for _, ports := range [][]string{h.Lifecycle, h.API} {
		seen := make(map[string]bool, len(ports))
		for _, port := range ports {
			switch port {
			case PortScheduler, PortOutboundHTTP, PortAuditRecord, PortHTTPRequest, PortObservationSample, PortObservationHistory, PortAuditHistory:
			default:
				return fmt.Errorf("unknown component host port %q", port)
			}
			if seen[port] {
				return fmt.Errorf("duplicate component host port %q", port)
			}
			seen[port] = true
		}
	}
	for _, port := range h.API {
		if port == PortScheduler || port == PortObservationSample {
			return fmt.Errorf("port %q belongs to component lifecycle", port)
		}
	}
	for _, port := range h.Lifecycle {
		if port == PortHTTPRequest || port == PortObservationHistory || port == PortAuditHistory {
			return fmt.Errorf("port %q belongs to component API routes", port)
		}
	}
	if len(h.DropData) > 1 || len(h.DropData) == 1 && h.DropData[0] != PortSettingsDrop {
		return fmt.Errorf("unsupported component data-drop ports")
	}
	return nil
}
