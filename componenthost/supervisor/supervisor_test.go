package supervisor

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

type recordingLifecycle struct {
	id     string
	events *[]string
}

type retryStopLifecycle struct {
	recordingLifecycle
	failures int
}

func (l *retryStopLifecycle) Stop(ctx context.Context) error {
	if err := l.recordingLifecycle.Stop(ctx); err != nil {
		return err
	}
	if l.failures > 0 {
		l.failures--
		return errors.New("stop failed")
	}
	return nil
}

func (l recordingLifecycle) Start(context.Context, lifecycle.Context) error {
	*l.events = append(*l.events, "start:"+l.id)
	return nil
}

func (l recordingLifecycle) Stop(context.Context) error {
	*l.events = append(*l.events, "stop:"+l.id)
	return nil
}

func (l recordingLifecycle) DropData(context.Context, lifecycle.Context) error {
	*l.events = append(*l.events, "drop:"+l.id)
	return nil
}

func TestSupervisorReconcileStartsAndStopsChangedComponents(t *testing.T) {
	var events []string
	components := []registry.Component{
		testComponent("alpha", &events),
		testComponent("beta", &events),
	}
	active := []registry.Component{components[0]}
	supervisor := New(lifecycleHostForTest())
	supervisor.activeComponents = func() ([]registry.Component, error) {
		return active, nil
	}

	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("idempotent start: %v", err)
	}
	active = []registry.Component{components[1]}
	if err := supervisor.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	want := []string{"start:alpha", "stop:alpha", "start:beta"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestSupervisorStopStopsRunningComponentsInReverseOrder(t *testing.T) {
	var events []string
	components := []registry.Component{
		testComponent("alpha", &events),
		testComponent("beta", &events),
	}
	supervisor := New(lifecycleHostForTest())
	supervisor.activeComponents = func() ([]registry.Component, error) {
		return components, nil
	}

	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	want := []string{"start:alpha", "start:beta", "stop:beta", "stop:alpha"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestSupervisorStopRetainsFailedComponentsForRetry(t *testing.T) {
	var events []string
	lifecycleValue := &retryStopLifecycle{recordingLifecycle: recordingLifecycle{id: "retry", events: &events}, failures: 1}
	component := registry.Component{Manifest: manifest.Manifest{ID: "retry", Name: "Retry", Version: "1", Delivery: manifest.DeliveryInProcess}, Lifecycle: lifecycleValue}
	supervisor := New(lifecycleHostForTest())
	supervisor.running = []registry.Component{component}
	if err := supervisor.Stop(context.Background()); err == nil {
		t.Fatal("failed stop was reported as success")
	}
	if len(supervisor.running) != 1 {
		t.Fatal("failed component was forgotten instead of retained for retry")
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.running) != 0 {
		t.Fatal("successfully retried component remained running")
	}
}

func TestSupervisorDropDataCallsComponentDropper(t *testing.T) {
	var events []string
	const componentID = "test-supervisor-drop"
	component := testComponent(componentID, &events)
	registry.Register(component)
	supervisor := New(lifecycleHostForTest())

	if err := supervisor.DropData(context.Background(), componentID); err != nil {
		t.Fatal(err)
	}
	if want := []string{"drop:" + componentID}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestSupervisorDropDataRequiresDurableOwnerLifecycle(t *testing.T) {
	const componentID = "test-missing-drop-lifecycle"
	registry.Register(registry.Component{
		Manifest: manifest.Manifest{ID: componentID, Name: "Missing Drop", Version: "1", Delivery: manifest.DeliveryInProcess,
			Database: manifest.Database{Tables: []string{"test_missing_drop_rows"}}},
		Lifecycle: lifecycle.Noop{},
	})
	if err := New(lifecycleHostForTest()).DropData(context.Background(), componentID); err == nil {
		t.Fatal("durable component without a data-drop lifecycle was accepted")
	}
}

func testComponent(id string, events *[]string) registry.Component {
	return registry.Component{
		Manifest: manifest.Manifest{
			ID:             id,
			Name:           id,
			Version:        "1",
			Delivery:       manifest.DeliveryInProcess,
			DefaultEnabled: true,
		},
		Lifecycle: recordingLifecycle{id: id, events: events},
	}
}

func lifecycleHostForTest() componenthost.Deps {
	return componenthost.Deps{}
}
