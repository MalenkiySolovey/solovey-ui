package supervisor

import (
	"context"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestRemovalRetainsFailedLifecycleAuthority(t *testing.T) {
	const id = "test-removal-authority"
	owner := &observedLifecycleFixture{stopErr: errors.New("stop failed")}
	s := New(lifecycleHostForTest())
	s.activeComponents = func() ([]registry.Component, error) {
		return []registry.Component{{Manifest: manifest.Manifest{ID: id}, Lifecycle: owner}}, nil
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	committed := false
	commit := func() error { committed = true; return nil }
	if err := s.TryAdmitRemoval(t.Context(), id, commit); err == nil || committed {
		t.Fatal("running component admitted removal")
	}
	if err := s.Stop(t.Context()); err == nil {
		t.Fatal("expected failed Stop")
	}
	if err := s.TryAdmitRemoval(t.Context(), id, commit); err == nil || committed {
		t.Fatal("failed Stop discarded lifecycle authority")
	}
	owner.stopErr = nil
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.TryAdmitRemoval(t.Context(), id, commit); err != nil || !committed {
		t.Fatalf("released instance remained blocked: %v", err)
	}
}

func TestRemovalFencesStartMigrationAndUnknownIncarnation(t *testing.T) {
	const id = "test-removal-fence"
	s := New(lifecycleHostForTest())
	called := false
	commit := func() error {
		called = true
		if s.mu.TryLock() {
			s.mu.Unlock()
			t.Error("Start was not excluded")
		}
		if componentMigrationMu.TryLock() {
			componentMigrationMu.Unlock()
			t.Error("migration was not excluded")
		}
		return nil
	}
	if err := s.TryAdmitRemoval(t.Context(), id, commit); err != nil || !called {
		t.Fatalf("clean current supervisor: %v", err)
	}
	called = false
	s.mu.Lock()
	err := s.TryAdmitRemoval(t.Context(), id, commit)
	s.mu.Unlock()
	if err == nil || called {
		t.Fatal("busy lifecycle admitted a commit")
	}
	componentMigrationMu.Lock()
	err = s.TryAdmitRemoval(t.Context(), id, commit)
	componentMigrationMu.Unlock()
	if err == nil || called {
		t.Fatal("busy migration admitted a commit")
	}
	s.observations.RecordStart(id, false)
	if err := s.TryAdmitRemoval(t.Context(), id, commit); err == nil || called {
		t.Fatal("unknown failed Start admitted a commit")
	}
	_ = state.NewLifecyclePublisher()
	if err := s.TryAdmitRemoval(t.Context(), id, commit); err == nil || called {
		t.Fatal("old supervisor incarnation admitted a commit")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.TryAdmitRemoval(ctx, id, commit); err == nil || called {
		t.Fatal("canceled removal admitted a commit")
	}
}
