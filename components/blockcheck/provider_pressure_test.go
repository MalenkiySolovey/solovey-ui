//go:build !minimal

package blockcheck

import (
	"testing"

	pressure "github.com/MalenkiySolovey/solovey-ui/internal/ops/resourcepressure"
)

func TestLocalBlockCheckProbeParallelism(t *testing.T) {
	for _, tt := range []struct {
		cpu   int
		state pressure.State
		want  int
	}{
		{1, pressure.StateNormal, 1},
		{2, pressure.StateNormal, 1},
		{5, pressure.StateNormal, 4}, // the feature applies its own ceiling
		{5, pressure.StateWarning, 2},
		{5, pressure.StateRecovering, 2},
		{5, pressure.StateConstrained, 1},
		{5, pressure.StateCritical, 1},
		{5, pressure.StateUnknown, 1},
	} {
		if got := localProbeParallelism(tt.cpu, tt.state); got != tt.want {
			t.Errorf("cpu=%d state=%s: parallelism %d, want %d", tt.cpu, tt.state, got, tt.want)
		}
	}
}
