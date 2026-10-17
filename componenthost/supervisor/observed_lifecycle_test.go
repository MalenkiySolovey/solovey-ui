package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

type observedLifecycleFixture struct {
	startErr   error
	stopErr    error
	panicStart bool
}

func (f *observedLifecycleFixture) Start(context.Context, lifecycle.Context) error {
	if f.panicStart {
		panic("owner-secret-start-panic")
	}
	return f.startErr
}

func (f *observedLifecycleFixture) Stop(context.Context) error { return f.stopErr }

func TestSupervisorPublishesActualCallsAndRetainsFailedStop(t *testing.T) {
	const id = "test-observed-lifecycle"
	owner := &observedLifecycleFixture{startErr: errors.New("owner-secret-start-error")}
	active := []registry.Component{{Manifest: manifest.Manifest{ID: id}, Lifecycle: owner}}
	supervisor := New(lifecycleHostForTest())
	supervisor.activeComponents = func() ([]registry.Component, error) { return active, nil }
	if err := supervisor.Start(context.Background()); err == nil {
		t.Fatal("expected Start error")
	}
	result := state.LifecycleForComponent(id, true, true)
	if result.Phase != state.LifecycleStartFailed || result.InstanceRetention != state.InstanceUnknown || len(supervisor.running) != 0 {
		t.Fatalf("Start failure became ready or released: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "owner-secret") {
		t.Fatalf("lifecycle projection leaked owner error: %s, %v", encoded, err)
	}
	owner.startErr = nil
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	result = state.LifecycleForComponent(id, true, true)
	if result.Phase != state.LifecycleStarted || result.InstanceRetention != state.InstanceRetained || result.ObservedAt == nil {
		t.Fatalf("successful call was not observed: %+v", result)
	}
	if err := supervisor.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state.LifecycleForComponent(id, true, true); !got.ObservedAt.Equal(*result.ObservedAt) {
		t.Fatal("idempotent reconcile fabricated a new call observation")
	}
	active = nil
	owner.stopErr = errors.New("owner-secret-stop-error")
	if err := supervisor.Reconcile(context.Background()); err == nil {
		t.Fatal("expected Stop error")
	}
	result = state.LifecycleForComponent(id, false, false)
	if result.Phase != state.LifecycleStopFailed || result.InstanceRetention != state.InstanceRetained || len(supervisor.running) != 1 {
		t.Fatalf("failed Stop authority was forgotten: %+v", result)
	}
	owner.stopErr = nil
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	result = state.LifecycleForComponent(id, false, true)
	if result.Phase != state.LifecycleStopped || result.InstanceRetention != state.InstanceReleased || len(supervisor.running) != 0 {
		t.Fatalf("Stop retry not observed: %+v", result)
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := state.LifecycleForComponent(id, false, true); !got.ObservedAt.Equal(*result.ObservedAt) {
		t.Fatal("empty Stop fabricated a new call observation")
	}
}

func TestSupervisorStartPanicPublishesOnlyFixedFailure(t *testing.T) {
	const id = "test-observed-panic"
	supervisor := New(lifecycleHostForTest())
	supervisor.activeComponents = func() ([]registry.Component, error) {
		return []registry.Component{{Manifest: manifest.Manifest{ID: id}, Lifecycle: &observedLifecycleFixture{panicStart: true}}}, nil
	}
	if err := supervisor.Start(context.Background()); err == nil {
		t.Fatal("panic was not contained")
	}
	result := state.LifecycleForComponent(id, true, true)
	if result.Phase != state.LifecycleStartFailed || result.ReasonCode != "START_FAILED" {
		t.Fatalf("panic result was not observed: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "owner-secret") {
		t.Fatalf("panic detail leaked into inventory: %s, %v", encoded, err)
	}
}
