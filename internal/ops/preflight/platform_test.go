package preflight

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func testFacts(now time.Time) domain.PlatformFacts {
	facts := domain.UnknownPlatformFacts("not_observed", now)
	facts.Source, facts.EvidenceRevision = domain.ProviderV1, domain.Revision("observation")
	facts.ObservedAt, facts.ExpiresAt = now.Unix(), now.Add(time.Minute).Unix()
	facts.ServiceSupervision = domain.PlatformFact{Availability: domain.Available, Backend: "systemd", Reason: "observed"}
	facts.Revision = ""
	facts.Revision = domain.Revision(facts)
	return facts
}

func TestCriticalPlatformFactsFailClosed(t *testing.T) {
	now := time.Unix(1900000000, 0)
	for _, test := range []struct {
		name   string
		mutate func(*domain.PlatformFacts)
		ready  bool
	}{
		{"current", func(*domain.PlatformFacts) {}, true},
		{"unknown", func(f *domain.PlatformFacts) { f.ServiceSupervision.Availability = domain.Unknown }, false},
		{"unavailable", func(f *domain.PlatformFacts) { f.ServiceSupervision.Availability = domain.Unavailable }, false},
		{"expired", func(f *domain.PlatformFacts) { f.ExpiresAt = now.Unix() }, false},
		{"future", func(f *domain.PlatformFacts) { f.ObservedAt = now.Add(time.Second).Unix() }, false},
		{"unbounded", func(f *domain.PlatformFacts) { f.ExpiresAt = now.Add(time.Hour).Unix() }, false},
		{"no evidence", func(f *domain.PlatformFacts) { f.EvidenceRevision = "" }, false},
		{"forged schema", func(f *domain.PlatformFacts) { f.Schema = "future" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := testFacts(now)
			test.mutate(&facts)
			facts.Revision = ""
			facts.Revision = domain.Revision(facts)
			report := Platform(t.Context(), facts, now, Requirement{Supervision, "systemd"})
			if report.Ready != test.ready {
				t.Fatalf("report=%+v", report)
			}
		})
	}
	facts := testFacts(now)
	if Platform(t.Context(), facts, now, Requirement{PersistentStorage, ""}).Ready || Platform(t.Context(), facts, now, Requirement{PackageOperations, ""}).Ready {
		t.Fatal("unobserved storage/package inferred from systemd")
	}
	if Platform(t.Context(), facts, now, Requirement{Supervision, "procd"}).Ready {
		t.Fatal("owner backend requirement ignored")
	}
	facts.ServiceSupervision.Availability = domain.Unavailable
	if Platform(t.Context(), facts, now, Requirement{Supervision, ""}).Ready {
		t.Fatal("revision mutation accepted")
	}
}

func TestCancelledAndMalformedPreflightRemainsBoundedAndRedacted(t *testing.T) {
	now := time.Unix(1900000000, 0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	facts := testFacts(now)
	if Platform(ctx, facts, now, Requirement{Supervision, ""}).Ready {
		t.Fatal("cancelled preflight accepted")
	}
	report := Platform(t.Context(), facts, now, Requirement{Dimension("Bearer deterministic fake secret"), ""})
	payload, _ := json.Marshal(report)
	if report.Ready || strings.Contains(string(payload), "fake secret") {
		t.Fatalf("malformed dimension escaped: %s", payload)
	}
	if Platform(t.Context(), facts, now).Ready {
		t.Fatal("empty prerequisites manufacture readiness")
	}
}
