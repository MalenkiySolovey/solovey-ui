package api

import (
	"encoding/json"
	"fmt"
	"testing"

	pressuredomain "github.com/MalenkiySolovey/solovey-ui/internal/ops/resourcepressure"
)

func BenchmarkIntegratedOperationsStatusProjection(b *testing.B) {
	owners := make([]ownerStatus, 256)
	for index := range owners {
		owners[index] = ownerStatus{
			ID: fmt.Sprintf("owner-%03d", index), Kind: "component", Installed: true, Available: true,
			Enabled: "DISABLED", DurableData: "PRESERVED", Backup: "SUPPORTED",
			Restore: "SUPPORTED", DropData: "PREVIEW_REQUIRED",
		}
	}
	signals := make([]pressuredomain.Signal, pressuredomain.MaxSignals)
	for index := range signals {
		signals[index] = pressuredomain.Signal{
			ID: fmt.Sprintf("fixture.signal.%02d", index), Status: pressuredomain.ProviderSupported,
			Value: float64(index), Unit: "count", ObservedAt: 1000, ExpiresAt: 1100,
		}
	}
	input := operationsStatusProjection{
		GeneratedAt: 1000,
		Pressure: pressuredomain.Snapshot{
			State: pressuredomain.StateWarning, Signals: signals,
			ReasonCodes: []string{"WARNING:fixture.signal.00"}, Revision: 11, ObservedAt: 1000,
		},
		SQLiteRuntime: map[string]any{"runtimeVersion": "3.53.4", "runtimePinned": true},
		SQLiteState:   "VERIFIED", MigrationState: "APPLIED", MigrationRows: 200,
		Owners: owners, BackupState: "AVAILABLE_WITH_BOUNDS",
		Update:     map[string]any{"state": "UPDATE_AVAILABLE", "sequence": 42},
		Deployment: map[string]any{"state": "OBSERVED"}, ReasonCodes: []string{},
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		projected := projectOperationsStatus(input)
		if encoded, err := json.Marshal(projected); err != nil || len(encoded) == 0 {
			b.Fatalf("projection bytes=%d err=%v", len(encoded), err)
		}
	}
}
