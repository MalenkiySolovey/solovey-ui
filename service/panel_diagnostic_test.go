package service

import (
	"errors"
	"slices"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
)

func TestLocalPanelDiagnosticDoesNotFollowCoreReplacement(t *testing.T) {
	original := DefaultRuntime()
	t.Cleanup(func() { SetDefaultRuntime(original) })
	start := func(tag string) *coreruntime.Core {
		core := coreruntime.NewCore()
		config := `{"log":{"disabled":true},"inbounds":[{"type":"mixed","tag":"` + tag + `","listen":"127.0.0.1"}],"outbounds":[{"type":"direct","tag":"direct"}]}`
		if err := core.Start([]byte(config)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = core.Stop() })
		return core
	}
	current := NewRuntime(start("old-core"))
	SetDefaultRuntime(current)
	old, err := OpenLocalPanelDiagnostic("old-core")
	if err != nil {
		t.Fatal(err)
	}
	current.SetCore(start("new-core"))
	if err := old.Current(); !errors.Is(err, coreruntime.ErrStaleGeneration) {
		t.Fatalf("old path is still usable after owner replacement: %v", err)
	}
	if _, err := old.Resolve(t.Context(), "fixture.invalid", "dns"); !errors.Is(err, coreruntime.ErrStaleGeneration) {
		t.Fatalf("old path attempted a lookup after owner replacement: %v", err)
	}
	if got := LocalPanelDiagnosticChoices(); !slices.Equal(got, []string{"new-core"}) {
		t.Fatalf("active ingress choices: %v", got)
	}
	newPath, err := OpenLocalPanelDiagnostic("new-core")
	if err != nil || newPath.Generation() == old.Generation() {
		t.Fatalf("did not open the new generation: %v", err)
	}
}
