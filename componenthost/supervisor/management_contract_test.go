package supervisor

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestManagementContractRemovalVectors(t *testing.T) {
	data, err := os.ReadFile("../../contracts/management/v1/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Cases []struct {
			ID, Kind, Mode, Phase, Retention string
			Current, Running, Accepted       bool
		}
	}
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, v := range set.Cases {
		if v.Kind != "removal" {
			continue
		}
		count++
		t.Run(v.ID, func(t *testing.T) {
			const id = "r6-removal-fixture"
			owner := &observedLifecycleFixture{}
			s := New(lifecycleHostForTest())
			s.activeComponents = func() ([]registry.Component, error) {
				return []registry.Component{{Manifest: manifest.Manifest{ID: id}, Lifecycle: owner}}, nil
			}
			if v.Mode == "start-failed" {
				owner.startErr = errors.New("fixture start failure")
			}
			if v.Mode != "never-started" && v.Mode != "old-publisher" {
				err := s.Start(t.Context())
				if (err != nil) != (v.Mode == "start-failed") {
					t.Fatalf("start=%v", err)
				}
			}
			if v.Mode == "stop-failed" {
				owner.stopErr = errors.New("fixture stop failure")
			}
			if v.Mode == "stopped" || v.Mode == "stop-failed" {
				err := s.Stop(t.Context())
				if (err != nil) != (v.Mode == "stop-failed") {
					t.Fatalf("stop=%v", err)
				}
			}
			if v.Mode == "old-publisher" {
				state.NewLifecyclePublisher()
			}
			observation, current := s.observations.Snapshot(id)
			if current != v.Current || current && (observation.Phase != v.Phase || observation.InstanceRetention != v.Retention) || (len(s.running) > 0) != v.Running {
				t.Fatalf("fixture owner facts differ: %+v current=%v running=%d", observation, current, len(s.running))
			}
			commits := 0
			err = s.TryAdmitRemoval(t.Context(), id, func() error { commits++; return nil })
			if (err == nil) != v.Accepted || commits != map[bool]int{true: 1, false: 0}[v.Accepted] {
				t.Fatalf("removal accepted=%v commits=%d err=%v", v.Accepted, commits, err)
			}
			owner.startErr, owner.stopErr = nil, nil
			_ = s.Stop(t.Context())
		})
	}
	if count != 6 {
		t.Fatalf("missing removal vectors: %d", count)
	}
}
