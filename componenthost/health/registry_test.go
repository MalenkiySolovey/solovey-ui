package health

import (
	"context"
	"testing"
	"time"
)

type testChecker struct {
	id    string
	check func(context.Context) Result
	match func(string) bool
}

func (c testChecker) ResourceID() string               { return c.id }
func (c testChecker) Check(ctx context.Context) Result { return c.check(ctx) }
func (c testChecker) Matches(resourceID string) bool {
	return c.match != nil && c.match(resourceID)
}

func TestRegistryBoundsChecksAndDoesNotExposeDetails(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Register(testChecker{id: "core:panel:web", check: func(ctx context.Context) Result {
		<-ctx.Done()
		return Result{Status: StatusOK, FactCode: "should_not_escape"}
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := registry.Check(ctx, "core:panel:web")
	if got.Status != StatusDegraded || got.FactCode != "health_check_timeout" {
		t.Fatalf("bounded result = %#v", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout duration = %s", elapsed)
	}
}

func TestRegistryReportsExactMissingCapability(t *testing.T) {
	got := NewRegistry().Check(context.Background(), "core:subscription:default")
	if got.Status != StatusMissingCapability || got.FactCode != "health_check_unavailable" {
		t.Fatalf("missing result = %#v", got)
	}
}

func TestRegistryRejectsDuplicateExactAuthority(t *testing.T) {
	registry := NewRegistry()
	unregister, err := registry.Register(testChecker{id: "resource", check: func(context.Context) Result { return Result{Status: StatusOK} }})
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	if _, err := registry.Register(testChecker{id: "resource", check: func(context.Context) Result { return Result{Status: StatusOK} }}); err == nil {
		t.Fatal("duplicate health authority was accepted")
	}
}

func TestRegistryFailsClosedForAmbiguousMatchers(t *testing.T) {
	registry := NewRegistry()
	check := func(context.Context) Result { return Result{Status: StatusOK} }
	match := func(string) bool { return true }
	if _, err := registry.Register(testChecker{id: "matcher-a", check: check, match: match}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(testChecker{id: "matcher-b", check: check, match: match}); err != nil {
		t.Fatal(err)
	}
	got := registry.Check(context.Background(), "dynamic-resource")
	if got.Status != StatusMissingCapability || got.FactCode != "health_check_ambiguous" {
		t.Fatalf("ambiguous matcher result = %#v", got)
	}
}
