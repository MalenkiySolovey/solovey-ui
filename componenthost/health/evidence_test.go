package health

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func evidenceResult(revision string, at time.Time) Result {
	return WithEvidence(Result{ResourceID: "core:panel:web", Status: StatusOK, Check: "panel_listener", FactCode: "listener_ready"},
		"core", RuntimeReady, "panel_listener", revision, at, 30*time.Second)
}
func requirement(revision string) Requirement {
	return Requirement{"core", "panel_listener", RuntimeReady, revision, 5 * time.Second}
}

func TestProofIsPropositionSpecificFreshAndRevisionBound(t *testing.T) {
	now := time.Now()
	revision := Revision("binary-version")
	result := evidenceResult(revision, now)
	if !result.Satisfies(requirement(revision), now) {
		t.Fatal("fresh matching owner proof rejected")
	}
	for _, request := range []Requirement{
		{"foreign", "panel_listener", RuntimeReady, revision, 5 * time.Second},
		{"core", "panel_listener", DataPlaneVerified, revision, 5 * time.Second},
		{"core", "panel_listener", RuntimeReady, Revision("replacement"), 5 * time.Second},
		{"core", "panel_listener", RuntimeReady, "", 5 * time.Second},
	} {
		if result.Satisfies(request, now) {
			t.Fatalf("unproven proposition promoted: %+v", request)
		}
	}
	if result.Satisfies(requirement(revision), now.Add(6*time.Second)) || result.Satisfies(requirement(revision), now.Add(-time.Second)) {
		t.Fatal("stale/future proof accepted")
	}
	legacy := Result{ResourceID: result.ResourceID, Status: StatusOK}
	if legacy.Satisfies(requirement(revision), now) {
		t.Fatal("legacy status promoted to evidence")
	}
	result.Evidence.Source = "Bearer deterministic fake secret"
	payload, _ := json.Marshal(normalize(result.ResourceID, result))
	if strings.Contains(string(payload), "fake secret") {
		t.Fatalf("malformed evidence escaped: %s", payload)
	}
}

func TestLateHealthCompletionCannotProveReplacementGeneration(t *testing.T) {
	registry := NewRegistry()
	entered, release := make(chan struct{}), make(chan struct{})
	old := Revision("old")
	unregister, err := registry.Register(testChecker{id: "core:panel:web", check: func(context.Context) Result {
		close(entered)
		<-release
		return evidenceResult(old, time.Now())
	}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan Result, 1)
	go func() { done <- registry.Require(t.Context(), "core:panel:web", requirement(old)) }()
	<-entered
	unregister()
	current := Revision("new")
	stop, err := registry.Register(testChecker{id: "core:panel:web", check: func(context.Context) Result { return evidenceResult(current, time.Now()) }})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	close(release)
	if result := <-done; result.Status == StatusOK || result.FactCode != "health_check_generation_replaced" {
		t.Fatalf("late evidence escaped: %+v", result)
	}
	if result := registry.Require(t.Context(), "core:panel:web", requirement(current)); result.Status != StatusOK {
		t.Fatalf("replacement proof failed: %+v", result)
	}
	if result := registry.Require(t.Context(), "core:panel:web", requirement(old)); result.Status == StatusOK {
		t.Fatal("old desired revision accepted")
	}
}

func TestUncooperativeCheckCannotCreateUnboundedWorkers(t *testing.T) {
	registry := NewRegistry()
	registry.slots = make(chan struct{}, 1)
	entered, release, drained := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_, err := registry.Register(testChecker{id: "resource", check: func(context.Context) Result {
		close(entered)
		<-release
		close(drained)
		return Result{Status: StatusOK}
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan Result, 1)
	go func() { done <- registry.Check(ctx, "resource") }()
	<-entered
	cancel()
	if result := <-done; result.FactCode != "health_check_timeout" {
		t.Fatalf("cancelled check=%+v", result)
	}
	if result := registry.Check(t.Context(), "resource"); result.FactCode != "health_check_busy" {
		t.Fatalf("worker limit bypassed: %+v", result)
	}
	close(release)
	<-drained
}
