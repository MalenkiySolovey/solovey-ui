package udpguard

import (
	"testing"

	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
)

func TestMissingProbeOrTransportRejectsBeforeBaselineAndState(t *testing.T) {
	for _, missing := range []string{"probes", "transports"} {
		t.Run(missing, func(t *testing.T) {
			c := &Controller{Repository: struct{ StateRepository }{}, Baseline: &protectionfirewall.BaselineService{}, Probes: struct{ ProbeSource }{}, Transports: struct{ TransportSource }{}}
			if missing == "probes" {
				c.Probes = nil
			} else {
				c.Transports = nil
			}
			if _, err := c.Status(t.Context(), true); ErrorCode(err) != CodeMissingCapability {
				t.Fatalf("missing owner admitted: %v", err)
			}
		})
	}
}
