package update

import (
	"errors"
	"testing"
)

func TestLegacyManagerRejectsEveryInProcessActivation(t *testing.T) {
	called := false
	manager := NewManager(ManagerOptions{
		CurrentVersion: func() string { return "1.0.0" },
		Pipeline: func(ReleaseTarget, PipelineDeps, func(UpdateStage)) error {
			called = true
			return nil
		},
	})
	err := manager.Apply(ReleaseTarget{Version: "2.0.0"}, "admin")
	if !errors.Is(err, ErrBrokerRequired) {
		t.Fatalf("error = %v", err)
	}
	if called || manager.InProgress() || manager.Status().Stage != UpdateStageIdle {
		t.Fatalf("legacy manager obtained authority: called=%v status=%#v", called, manager.Status())
	}
}

func TestLegacyManagerStillRejectsInvalidOrNonNewerVersionFirst(t *testing.T) {
	manager := NewManager(ManagerOptions{CurrentVersion: func() string { return "2.0.0" }})
	if err := manager.Apply(ReleaseTarget{Version: "invalid"}, "admin"); err == nil {
		t.Fatal("invalid version accepted")
	}
	if err := manager.Apply(ReleaseTarget{Version: "2.0.0"}, "admin"); !errors.Is(err, ErrNotNewer) {
		t.Fatalf("non-newer error = %v", err)
	}
}
