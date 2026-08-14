package service

import (
	"testing"

	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"
)

func TestIntegrationStatsPipelineNilCoreSmoke(t *testing.T) {
	initSettingTestDB(t)
	runtime := NewRuntimeWithCoreProvider(nil)
	replaceDefaultRuntimeForTest(t, runtime)
	ipmonitor.ResetCaches()

	if err := (&StatsService{Runtime: runtime}).SaveStats(true); err != nil {
		t.Fatalf("SaveStats with nil core should be a no-op, got %v", err)
	}
}
