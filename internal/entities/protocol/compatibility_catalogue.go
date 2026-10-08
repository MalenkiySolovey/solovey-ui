package protocol

import "github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"

func CompatibilityCatalogue() []diagnostics.CompatibilityFact {
	return []diagnostics.CompatibilityFact{
		{"DEP-10", "Hysteria inbound recv_window_client/recv_window_conn/max_conn_client/disable_mtu_discovery; outbound recv_window/recv_window_conn/disable_mtu_discovery", "DEPRECATED_ACCEPTED", "Normalize at the actual side-specific consumer; preserve zero/false and reject divergent aliases."},
	}
}
