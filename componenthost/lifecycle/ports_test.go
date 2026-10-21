package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

type portOwner struct {
	started, stopped, dropped int
	stopErr                   error
}

func (o *portOwner) Start(_ context.Context, ports componenthost.Ports) error {
	if !ports.Host.IsLocal() {
		return errors.New("incorrect host")
	}
	o.started++
	return nil
}
func (o *portOwner) Stop(context.Context) error                          { o.stopped++; return o.stopErr }
func (o *portOwner) DropData(context.Context, componenthost.Ports) error { o.dropped++; return nil }

func TestPortLifecycleUsesExistingSupervisorContractAndDropOwner(t *testing.T) {
	owner := &portOwner{stopErr: errors.New("not drained")}
	contract := manifest.Manifest{ID: "typed-fixture", Name: "Typed fixture", Delivery: manifest.DeliveryInProcess, HostAPI: &manifest.HostAPI{Version: manifest.HostAPIV1}}
	adapter, err := BindPorts(contract, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(t.Context(), Context{}); err != nil || owner.started != 1 {
		t.Fatal(err)
	}
	if err := adapter.Stop(t.Context()); !errors.Is(err, owner.stopErr) {
		t.Fatal("stop error lost")
	}
	owner.stopErr = nil
	if err := adapter.Stop(t.Context()); err != nil || owner.stopped != 2 {
		t.Fatal("stop retry lost", err)
	}
	if err := adapter.(DataDropper).DropData(t.Context(), Context{}); err != nil || owner.dropped != 1 {
		t.Fatal("drop owner lost", err)
	}
	contract.HostAPI.Lifecycle = []string{manifest.PortScheduler}
	adapter, err = BindPorts(contract, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(t.Context(), Context{}); err == nil || owner.started != 1 {
		t.Fatal("missing port reached owner")
	}
}

type legacyMigrationOwner struct{ portOwner }

func (*legacyMigrationOwner) Migrate(context.Context, Context) error { return nil }

func TestTypedLifecycleRejectsLegacyHookRatherThanDroppingIt(t *testing.T) {
	contract := manifest.Manifest{ID: "typed-fixture", Name: "Typed fixture", Delivery: manifest.DeliveryInProcess, HostAPI: &manifest.HostAPI{Version: manifest.HostAPIV1}}
	if _, err := BindPorts(contract, &legacyMigrationOwner{}); err == nil {
		t.Fatal("legacy migration silently discarded")
	}
}
