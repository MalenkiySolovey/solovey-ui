package operations

import (
	"context"
	"errors"
	"testing"
)

// Every journal method is intentionally absent: invoking one after rejected
// admission would panic through the embedded interface and fail the test.
type rejectedMaintenanceStore struct {
	Store
	admitted context.Context
	release  func()
	err      error
}

func (s rejectedMaintenanceStore) AdmitMaintenance(context.Context) (context.Context, func(), error) {
	return s.admitted, s.release, s.err
}

func TestMissingJournalOwnerCannotHeartbeatOrRecover(t *testing.T) {
	manager := NewManager(nil, Options{})
	if _, err := manager.Heartbeat(t.Context(), "owned", 1); err == nil {
		t.Fatal("missing owner admitted a heartbeat")
	}
	if _, err := manager.Recover(t.Context()); err == nil {
		t.Fatal("missing owner admitted recovery")
	}
}

func TestIncompleteJournalAdmissionReleasesWithoutReadingOrWriting(t *testing.T) {
	for _, nilContext := range []bool{true, false} {
		releases := 0
		store := rejectedMaintenanceStore{admitted: t.Context()}
		if nilContext {
			store.admitted, store.release = nil, func() { releases++ }
		}
		manager := NewManager(store, Options{})
		if _, err := manager.Recover(t.Context()); err == nil {
			t.Fatal("incomplete owner admission started recovery")
		}
		if nilContext && releases != 1 || !nilContext && releases != 0 {
			t.Fatalf("incomplete admission retained its release: %d", releases)
		}
	}
}

func TestCanceledJournalAdmissionNeverReachesHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	releases := 0
	manager := NewManager(rejectedMaintenanceStore{admitted: ctx, release: func() { releases++ }}, Options{})
	if _, err := manager.Heartbeat(t.Context(), "owned", 1); !errors.Is(err, context.Canceled) || releases != 1 {
		t.Fatalf("canceled admission reached journal or retained lease: releases=%d err=%v", releases, err)
	}
}
