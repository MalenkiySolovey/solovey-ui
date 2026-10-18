package state

import (
	"fmt"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
)

func TestLifecycleWithoutResultsNeverClaimsReadinessOrRelease(t *testing.T) {
	NewLifecyclePublisher()
	for _, fixture := range []struct {
		name      string
		installed bool
		available bool
		phase     string
	}{
		{"installed", true, true, LifecycleNotObserved},
		{"binary absent", true, false, LifecycleBinaryAbsent},
		{"not installed", false, true, LifecycleNotInstalled},
		{"removed and absent", false, false, LifecycleNotInstalled},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			result := LifecycleForComponent("fixture", fixture.installed, fixture.available)
			if result.Phase != fixture.phase || result.InstanceRetention != InstanceUnknown || result.ObservedAt != nil || result.Source == "" || result.ReasonCode == "" {
				t.Fatalf("unsupported lifecycle authority: %+v", result)
			}
		})
	}
}

func TestLifecycleEpochRejectsLateResultsAndClonesTime(t *testing.T) {
	old := NewLifecyclePublisher()
	old.RecordStart("fixture", true)
	current := NewLifecyclePublisher()
	old.RecordStop("fixture", true)
	if got := LifecycleForComponent("fixture", true, true); got.Phase != LifecycleNotObserved {
		t.Fatalf("old incarnation published into new one: %+v", got)
	}
	current.RecordStop("fixture", false)
	result := LifecycleForComponent("fixture", false, false)
	if result.Phase != LifecycleStopFailed || result.InstanceRetention != InstanceRetained || result.ObservedAt == nil || result.Source != "componenthost.supervisor" {
		t.Fatalf("metadata hid retained failed Stop: %+v", result)
	}
	observedAt := *result.ObservedAt
	*result.ObservedAt = time.Time{}
	if got := LifecycleForComponent("fixture", true, true); !got.ObservedAt.Equal(observedAt) {
		t.Fatal("reader modified the retained observation")
	}
}

func TestLifecycleObservationStorageIsBounded(t *testing.T) {
	publisher := NewLifecyclePublisher()
	for i := 0; i < installstate.MaxInstalledComponents+10; i++ {
		publisher.RecordStart(fmt.Sprintf("fixture-%03d", i), true)
	}
	publisher.RecordStart("invalid/id", true)
	lifecycleResults.RLock()
	count := len(lifecycleResults.results)
	lifecycleResults.RUnlock()
	if count != installstate.MaxInstalledComponents {
		t.Fatalf("unexpected observation count: %d", count)
	}
	if got := LifecycleForComponent("fixture-256", true, true); got.Phase != LifecycleNotObserved || got.InstanceRetention != InstanceUnknown {
		t.Fatalf("unrecorded owner became known: %+v", got)
	}
	publisher.RecordStop("fixture-000", true)
	if got := LifecycleForComponent("fixture-000", true, true); got.Phase != LifecycleStopped {
		t.Fatalf("capacity prevented updating an existing owner: %+v", got)
	}
}
