package managementcontract

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	wire "github.com/MalenkiySolovey/solovey-ui/contracts/management/v1"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	ops "github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

func TestEveryCurrentComponentManifestHasTheSamePublicIdentity(t *testing.T) {
	files, err := filepath.Glob("../../components/*/component.json")
	if err != nil || len(files) != 8 {
		t.Fatalf("catalog=%d err=%v", len(files), err)
	}
	for _, name := range files {
		t.Run(filepath.Base(filepath.Dir(name)), func(t *testing.T) {
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			var actual manifest.Manifest
			if err := json.Unmarshal(raw, &actual); err != nil {
				t.Fatal(err)
			}
			if err := actual.Validate(); err != nil {
				t.Fatal(err)
			}
			publisher := state.NewLifecyclePublisher()
			publisher.RecordStart(actual.ID, true)
			lifecycle, current := publisher.Snapshot(actual.ID)
			if !current {
				t.Fatal("current publisher disappeared")
			}
			value := state.Component{ID: actual.ID, Installed: true, Enabled: true, Active: true, AvailableInBinary: true, ObservedLifecycle: lifecycle}
			projected, err := ProjectComponent(actual, value, nil, health.Requirement{}, "", time.Unix(1700000000, 0))
			if err != nil || projected.Manifest.ComponentID != actual.ID || projected.Manifest.Version != actual.Version || projected.State != "UNKNOWN" {
				t.Fatalf("manifest projection=%+v err=%v", projected, err)
			}
			if len(projected.Manifest.Actions) != len(actual.Actions) {
				t.Fatal("Actions changed")
			}
			for i, id := range actual.Actions {
				if projected.Manifest.Actions[i] != id {
					t.Fatal("Action identity changed")
				}
				if _, ok := Action(id); !ok {
					t.Fatal("manifest action absent from actual registry")
				}
			}
			encoded, err := json.Marshal(projected)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"frontend", "hostAPI", "database", "tokenScopes", "privilegedBroker", "entries"} {
				if strings.Contains(string(encoded), `"`+private+`"`) {
					t.Fatalf("private manifest field exported: %s", private)
				}
			}
		})
	}
}

func TestJobProjectionCopiesOwnerTruthAndRejectsUnrepresentableState(t *testing.T) {
	change := ops.Change{Reference: ops.NewReference("data", "database", "operation:fixture"), Revision: 17, Phase: "RESTORING", OwnerPhase: "RESTORING", Outcome: ops.OutcomePending}
	job := ops.ProjectJob(change, &ops.Progress{Completed: 10, Total: 20, Unit: "bytes"}, true)
	cancelled := false
	job.CancelRequested = &cancelled
	job.Deadline = 1700000300
	projected, err := ProjectJob(job)
	if err != nil {
		t.Fatal(err)
	}
	if projected.CancelRequested == nil || *projected.CancelRequested || projected.Deadline == nil || *projected.Deadline != job.Deadline || projected.CancelSupported {
		t.Fatal("invented or lost cancellation/deadline truth")
	}
	*projected.CancelRequested = true
	projected.Progress.Completed = 20
	if *job.CancelRequested || job.Progress.Completed != 10 {
		t.Fatal("read projection aliases mutable owner state")
	}
	job.Revision = 0
	if _, err := ProjectJob(job); err == nil {
		t.Fatal("zero owner generation became a public counter")
	}
	change.HostRef = hostref.Ref{Kind: "NODE", ID: "node:foreign"}
	if _, err := ProjectChange(change); err == nil {
		t.Fatal("Local adapter invented remote operation truth")
	}
}

func TestExistingOwnerCASRejectsOldCompletionAndCancelledMutation(t *testing.T) {
	fixture := newFixtureOwner(t, commandCase{})
	conflict := errors.New("revision conflict")
	commit := func(ctx context.Context) error {
		return ops.CommitTransition(ctx, fixture.db, &fixtureRow{}, ops.Fence{OperationID: "fixture", Revision: 1, State: "READY"}, map[string]any{"revision": uint64(2), "state": "READY"}, &fixtureJournal{OperationID: "fixture", Revision: 2}, conflict)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !errors.Is(commit(ctx), context.Canceled) {
		t.Fatal("cancelled operation mutated owner state")
	}
	if err := commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(commit(t.Context()), conflict) {
		t.Fatal("late old revision committed")
	}
	var count int64
	if err := fixture.db.Model(&fixtureJournal{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("journal count=%d err=%v", count, err)
	}
}

type generationChecker struct {
	result  health.Result
	entered chan struct{}
	release chan struct{}
}

func (c generationChecker) ResourceID() string { return c.result.ResourceID }
func (c generationChecker) Check(context.Context) health.Result {
	if c.entered != nil {
		close(c.entered)
		<-c.release
	}
	return c.result
}

func TestRealHealthRegistryFencesReplacedGenerationWithoutChangingRevision(t *testing.T) {
	registry := health.NewRegistry()
	entered, release := make(chan struct{}), make(chan struct{})
	revision := strings.Repeat("a", 64)
	at := time.Unix(1700000000, 0)
	value := health.WithEvidence(health.Result{ResourceID: "resource:fixture", Status: health.StatusOK, Check: "fixture", FactCode: "fixture_ok"}, "test-owner", health.ProbeOK, "fixture", revision, at, time.Minute)
	stop, err := registry.Register(generationChecker{value, entered, release})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan health.Result, 1)
	go func() { done <- registry.Check(t.Context(), value.ResourceID) }()
	<-entered
	stop()
	stopCurrent, err := registry.Register(generationChecker{result: value})
	if err != nil {
		t.Fatal(err)
	}
	defer stopCurrent()
	close(release)
	old := <-done
	if old.Status == health.StatusOK || old.FactCode != "health_check_generation_replaced" {
		t.Fatal("old checker escaped epoch fence")
	}
	requirement := health.Requirement{Owner: "test-owner", Source: "fixture", ProofKind: health.ProbeOK, DesiredRevision: revision, MaxAge: time.Minute}
	projected, err := ProjectHealth(old, requirement, at)
	if err != nil || projected.Status == "PASS" {
		t.Fatal("old-generation result became fresh public Health")
	}
	current, err := ProjectHealth(registry.Check(t.Context(), value.ResourceID), requirement, at)
	if err != nil || current.Status != "PASS" || current.Evidence.RuntimeGeneration != nil {
		t.Fatalf("current evidence=%+v err=%v", current, err)
	}
}

func TestAllDomainReferencesPreserveCurrentConfiguredOwnerIdentity(t *testing.T) {
	count := 0
	for _, raw := range vectorCases(t) {
		var vector struct {
			ID       string
			Document json.RawMessage
		}
		if err := json.Unmarshal(raw, &vector); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(vector.ID, "owner-ref-") {
			continue
		}
		count++
		var scope wire.ResourceRef
		if err := wire.Decode("ResourceRef", vector.Document, &scope); err != nil {
			t.Fatal(err)
		}
		t.Run(scope.Owner, func(t *testing.T) {
			resource := resources.ProtectableResource{ID: scope.ResourceID, Owner: scope.Owner, Source: "fixture", Capabilities: resources.ProtectableResourceCapabilities{Known: true, ConfigRevision: "owner-revision:a"}}
			if _, _, err := ProjectConfiguredResource(resource); err == nil {
				t.Fatal("legacy absence invented Local authority")
			}
			resource.Identity = resources.LocalConfiguredIdentity(resource)
			projected, revision, err := ProjectConfiguredResource(resource)
			if err != nil || projected.Host != LocalHost() || projected.Owner != scope.Owner || projected.ResourceID != scope.ResourceID || revision != "owner-revision:a" {
				t.Fatalf("identity=%+v revision=%s err=%v", projected, revision, err)
			}
			if resource.Identity.ObservedRevision != "" || resource.Identity.ObservationStatus != resources.ResourceObservationUnknown {
				t.Fatal("configured identity invented observation")
			}
			resource.Capabilities.ConfigRevision = "owner-revision:b"
			if _, _, err := ProjectConfiguredResource(resource); err == nil {
				t.Fatal("stale configured revision accepted")
			}
			resource.Capabilities.ConfigRevision = "owner-revision:a"
			resource.Identity.Host = hostref.Ref{Kind: scope.Host.Kind, ID: scope.Host.ID}
			if _, _, err := ProjectConfiguredResource(resource); err == nil {
				t.Fatal("Local adapter invented remote resource authority")
			}
		})
	}
	if count != 7 {
		t.Fatalf("owner reference coverage=%d", count)
	}
}
