package observation

import (
	"context"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/componenthost/publicsurface"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/observationio"
)

type observationPortStoreFixture struct {
	buffer int
	cycles int
}

func (s *observationPortStoreFixture) LoadSettings(context.Context) (domain.Settings, bool, error) {
	return domain.Settings{ObservationBufferSize: s.buffer}, false, nil
}
func (s *observationPortStoreFixture) BeginObservation(context.Context) (observationio.Cycle, error) {
	s.cycles++
	return nil, nil
}

func TestUnconfiguredObservationPortsCannotStartIntake(t *testing.T) {
	store := &observationPortStoreFixture{buffer: 1}
	worker := NewWorker()
	if err := worker.Start(store); err == nil {
		t.Fatal("missing source/inventory admitted a worker")
	}
	if worker.Status().Running || store.cycles != 0 {
		t.Fatal("unavailable owner ports started work")
	}
}

func TestDisabledObservationBufferNeedsNoSourceAndStartsNoWorker(t *testing.T) {
	store := &observationPortStoreFixture{}
	worker := NewWorker()
	if err := worker.Start(store); err != nil {
		t.Fatal(err)
	}
	if worker.Status().Running || store.cycles != 0 {
		t.Fatal("disabled buffer started a worker")
	}
	if err := worker.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMissingObservationCycleCannotClaimPersistence(t *testing.T) {
	store := &observationPortStoreFixture{}
	worker := NewWorker()
	worker.repository = store
	count, err := worker.processBatch(t.Context(), []publicsurface.Observation{{ResourceID: "owned:surface"}})
	if err == nil || count != 0 || store.cycles != 1 {
		t.Fatalf("missing cycle became persistence: count=%d cycles=%d err=%v", count, store.cycles, err)
	}
}

type unavailableObservationSource struct{ releases int }

func (s *unavailableObservationSource) SubscribeObservations(int) (observationio.Subscription, func(), error) {
	return nil, func() { s.releases++ }, nil
}

type emptyObservationInventory struct{}

func (emptyObservationInventory) Snapshot(context.Context) hostresources.ResourceSnapshot {
	return hostresources.ResourceSnapshot{}
}
func (emptyObservationInventory) Refresh(context.Context) hostresources.ResourceSnapshot {
	return hostresources.ResourceSnapshot{}
}

func TestUnusableObservationSubscriptionReleasesAdmissionWithoutStartingWorker(t *testing.T) {
	source := &unavailableObservationSource{}
	worker := NewWorker(Options{Source: source, Inventory: emptyObservationInventory{}})
	if err := worker.Start(&observationPortStoreFixture{buffer: 1}); err == nil {
		t.Fatal("unusable subscription started an observation worker")
	}
	if worker.Status().Running || source.releases != 1 {
		t.Fatalf("unusable intake retained ownership: running=%v releases=%d", worker.Status().Running, source.releases)
	}
}
