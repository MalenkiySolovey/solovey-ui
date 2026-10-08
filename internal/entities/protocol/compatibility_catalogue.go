package protocol

import "github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"

func CompatibilityCatalogue() []diagnostics.CompatibilityFact {
	return []diagnostics.CompatibilityFact{
		{ID: "DEP-10", Consumer: "Hysteria inbound recv_window_client/recv_window_conn/max_conn_client/disable_mtu_discovery; outbound recv_window/recv_window_conn/disable_mtu_discovery", Classification: "DEPRECATED_ACCEPTED", Policy: "Normalize at the actual side-specific consumer; preserve zero/false and reject divergent aliases."},
	}
}
