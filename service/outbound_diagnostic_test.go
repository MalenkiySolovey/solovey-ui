package service

import (
	"errors"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
)

func TestLocalOutboundDiagnosticRetiresWithItsCore(t *testing.T) {
	previous := DefaultRuntime()
	t.Cleanup(func() { SetDefaultRuntime(previous) })
	original := new(coreruntime.Core)
	selected := NewRuntime(original)
	SetDefaultRuntime(selected)
	handle := LocalOutboundDiagnostic{source: original}
	if err := handle.Current(); !errors.Is(err, coreruntime.ErrCoreUnavailable) {
		t.Fatalf("current owner did not reach the Core handle: %v", err)
	}
	selected.SetCore(new(coreruntime.Core))
	if err := handle.Current(); !errors.Is(err, coreruntime.ErrStaleGeneration) {
		t.Fatalf("replaced Core left the diagnostic current: %v", err)
	}
}
