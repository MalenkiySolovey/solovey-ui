package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerRejectsConcurrentApply(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	manager := NewManager(ManagerOptions{
		CurrentVersion: func() string { return "1.0.0" },
		PipelineDeps:   func() PipelineDeps { return PipelineDeps{} },
		Pipeline: func(ReleaseTarget, PipelineDeps, func(UpdateStage)) error {
			close(started)
			<-release
			return errors.New("stop")
		},
	})
	target := ReleaseTarget{Version: "2.0.0"}
	if err := manager.Apply(target, "admin"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := manager.Apply(target, "admin"); !errors.Is(err, ErrUpdateInProgress) {
		t.Fatalf("second apply error = %v", err)
	}
	close(release)
}

func TestManagerFailureReleasesGuardAndAudits(t *testing.T) {
	audited := make(chan string, 1)
	manager := NewManager(ManagerOptions{
		CurrentVersion: func() string { return "1.0.0" },
		PipelineDeps:   func() PipelineDeps { return PipelineDeps{} },
		Pipeline:       func(ReleaseTarget, PipelineDeps, func(UpdateStage)) error { return errors.New("secret=boom") },
		TerminalAudit:  func(_ UpdateJob, result, _ string) { audited <- result },
	})
	if err := manager.Apply(ReleaseTarget{Version: "2.0.0"}, "admin"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-audited:
		if result != "failed" {
			t.Fatalf("audit result = %q", result)
		}
	case <-time.After(time.Second):
		t.Fatal("missing terminal audit")
	}
	if manager.InProgress() || manager.Status().Stage != UpdateStageFailed {
		t.Fatalf("status = %#v", manager.Status())
	}
}

func TestManagerWritesPendingMarkerBeforeExit(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "solovey-ui")
	if err := os.WriteFile(executable, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	manager := NewManager(ManagerOptions{
		CurrentVersion: func() string { return "1.0.0" },
		PipelineDeps:   func() PipelineDeps { return PipelineDeps{ExecPath: executable} },
		Pipeline:       func(ReleaseTarget, PipelineDeps, func(UpdateStage)) error { return nil },
		Exit:           func() { close(exited) },
	})
	if err := manager.Apply(ReleaseTarget{Version: "2.0.0"}, "admin"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("manager did not reach exit")
	}
	if _, err := os.Stat(executable + pendingSuffix); err != nil {
		t.Fatalf("pending marker missing: %v", err)
	}
}
