package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func platformPosture(now time.Time, profileID domain.ProfileID) domain.Posture {
	p := (&fakeProvider{now: now}).posture(profileID)
	if p.Runtime != domain.RuntimeSystemdNative {
		p.Systemd = nil
	}
	domain.SetPostureRevision(&p)
	return p
}

func TestPlatformSemanticFactsProviderEvidence(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, test := range []struct {
		name, provider, backend string
		profile                 domain.ProfileID
		supervision, broker     domain.Availability
	}{
		{"systemd", domain.ProviderV1, "systemd", domain.NativeHardened, domain.Available, domain.Available},
		{"native advanced", domain.ProviderV1, "systemd", domain.NativeNetworkAdvanced, domain.Available, domain.Available},
		{"legacy root", domain.ProviderV1, "systemd", domain.NativeLegacyRoot, domain.Available, domain.Available},
		{"procd", "package-managed-openwrt-posture/v1", "procd", domain.PackageManagedOpenWrt, domain.Available, domain.Available},
		{"docker host", "local-docker-posture/v1", "container-runtime", domain.DockerHost, domain.Unknown, domain.Unavailable},
		{"docker bridge", "local-docker-posture/v1", "container-runtime", domain.DockerBridge, domain.Unknown, domain.Unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := platformPosture(now, test.profile)
			facts := domain.ProjectPlatformFacts(p, test.provider, now)
			if facts.ServiceSupervision.Availability != test.supervision || facts.ServiceSupervision.Backend != test.backend ||
				facts.PrivilegeExecution.Availability != test.broker || facts.Source != test.provider ||
				facts.EvidenceRevision != p.Revision || facts.ObservedAt != p.ObservedAt || facts.ExpiresAt != p.ExpiresAt {
				t.Fatalf("facts=%+v", facts)
			}
			for _, unknown := range []domain.PlatformFact{facts.PersistentStorage, facts.PackageOperations, facts.NetworkObservation} {
				if unknown.Availability != domain.Unknown || unknown.Backend != "unknown" || unknown.Reason == "" {
					t.Fatalf("opaque digest became a capability: %+v", unknown)
				}
			}
			if p.Systemd != nil {
				if facts.Sandbox.Availability != domain.Available || facts.Sandbox.NoNewPrivileges == nil ||
					*facts.Sandbox.NoNewPrivileges != p.Systemd.NoNewPrivileges {
					t.Fatalf("systemd sandbox=%+v", facts.Sandbox)
				}
			} else if facts.Sandbox.Availability != domain.Unknown || facts.Sandbox.NoNewPrivileges != nil {
				t.Fatal("unobserved sandbox became known")
			}
		})
	}
}

func TestPlatformSemanticFactsInvalidEvidenceStaysUnknown(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, test := range []struct {
		name, provider string
		change         func(*domain.Posture)
		at             time.Time
	}{
		{"unknown provider", "unrecognized", nil, now},
		{"mismatched provider", "local-docker-posture/v1", nil, now},
		{"expired", domain.ProviderV1, nil, now.Add(3 * time.Minute)},
		{"future", domain.ProviderV1, nil, now.Add(-time.Minute)},
		{"tampered", domain.ProviderV1, func(p *domain.Posture) { p.DataRevision = domain.Revision("tampered") }, now},
		{"missing systemd", domain.ProviderV1, func(p *domain.Posture) { p.Systemd = nil; domain.SetPostureRevision(p) }, now},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := platformPosture(now, domain.NativeHardened)
			if test.change != nil {
				test.change(&p)
			}
			facts := domain.ProjectPlatformFacts(p, test.provider, test.at)
			if facts.Source != "unavailable" || facts.EvidenceRevision != "" || facts.ObservedAt != 0 {
				t.Fatalf("untrusted evidence published: %+v", facts)
			}
			for _, fact := range []domain.PlatformFact{facts.ServiceSupervision, facts.PrivilegeExecution, facts.PersistentStorage,
				facts.PackageOperations, facts.UpdateLifecycle, facts.NetworkObservation, facts.Sandbox.PlatformFact} {
				if fact.Availability != domain.Unknown || fact.Reason == "" {
					t.Fatalf("bad evidence promoted: %+v", fact)
				}
			}
		})
	}
}

func TestPlatformMissingBrokerNeverFallsBackToRoot(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, profile := range []domain.ProfileID{domain.NativeHardened, domain.NativeLegacyRoot, domain.PackageManagedOpenWrt} {
		p := platformPosture(now, profile)
		p.BrokerAvailable, p.BrokerRevision = false, ""
		domain.SetPostureRevision(&p)
		provider := domain.ProviderV1
		if profile == domain.PackageManagedOpenWrt {
			provider = "package-managed-openwrt-posture/v1"
		}
		facts := domain.ProjectPlatformFacts(p, provider, now)
		if facts.PrivilegeExecution.Availability != domain.Unavailable || facts.ServiceSupervision.Availability != domain.Available {
			t.Fatalf("profile=%s facts=%+v", profile, facts)
		}
		p.BrokerAvailable = true
		domain.SetPostureRevision(&p)
		if got := domain.ProjectPlatformFacts(p, provider, now).PrivilegeExecution.Availability; got != domain.Unknown {
			t.Fatalf("missing broker revision granted availability: %s", got)
		}
	}
}

func TestPlatformFactsPreservePostureWireAndCloneUnitFacts(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	p := platformPosture(now, domain.NativeHardened)
	p.Systemd.BoundingCapabilities = []string{"CAP_NET_BIND_SERVICE"}
	p.Systemd.Revision = ""
	p.Systemd.Revision = domain.Revision(*p.Systemd)
	domain.SetPostureRevision(&p)
	before, _ := json.Marshal(p)
	facts := domain.ProjectPlatformFacts(p, domain.ProviderV1, now)
	if len(facts.Sandbox.BoundingCapabilities) != 1 {
		t.Fatalf("missing observation: %+v", facts)
	}
	facts.Sandbox.BoundingCapabilities[0] = "changed"
	*facts.Sandbox.NoNewPrivileges = false
	after, _ := json.Marshal(p)
	if !bytes.Equal(before, after) || p.Validate(now) != nil {
		t.Fatal("semantic projection changed released Posture bytes/revision")
	}
	first := domain.ProjectPlatformFacts(p, domain.ProviderV1, now)
	second := domain.ProjectPlatformFacts(p, domain.ProviderV1, now)
	if first.Revision != second.Revision {
		t.Fatal("projection revision is nondeterministic")
	}
}

type platformReadProvider struct {
	*fakeProvider
	reads     int
	deadline  bool
	err       error
	lifecycle UpdateLifecycle
}

func (p *platformReadProvider) UpdateLifecycle() UpdateLifecycle { return p.lifecycle }

func (p *platformReadProvider) Observe(ctx context.Context) (domain.Posture, error) {
	p.reads++
	_, p.deadline = ctx.Deadline()
	return p.posture(p.current), p.err
}

func TestPlatformFactsReadHasNoPersistenceOrLifecycleEffects(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	p := &platformReadProvider{fakeProvider: &fakeProvider{now: now, current: domain.NativeHardened}, lifecycle: UpdateLifecycleSelfManaged}
	// No repository/DB: this read must not call SavePosture or any lifecycle path.
	m := NewManager(Repository{}, p)
	m.Now = func() time.Time { return now }
	if facts := m.PlatformFacts(context.Background()); facts.Source != domain.ProviderV1 || p.reads != 1 || !p.deadline || len(p.mutations) != 0 {
		t.Fatalf("facts=%+v provider=%+v", facts, p)
	}
	for _, lifecycle := range []UpdateLifecycle{UpdateLifecycleSelfManaged, UpdateLifecyclePackageManaged, UpdateLifecycleOperatorManaged} {
		p.lifecycle = lifecycle
		facts := m.PlatformFacts(context.Background())
		if facts.UpdateLifecycle.Availability != domain.Available || facts.UpdateLifecycle.Backend != string(lifecycle) ||
			facts.PackageOperations.Availability != domain.Unknown {
			t.Fatalf("lifecycle contract not preserved: %+v", facts)
		}
	}
	reads := p.reads
	m.observeMu.Lock()
	facts := m.PlatformFacts(context.Background())
	m.observeMu.Unlock()
	if facts.ServiceSupervision.Reason != "platform_observation_busy" || p.reads != reads {
		t.Fatal("busy read queued or observed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := m.PlatformFacts(ctx); got.ServiceSupervision.Reason != "platform_observation_cancelled" || p.reads != reads {
		t.Fatal("cancelled read observed")
	}
	p.err = errors.New("private-provider-path-and-credential")
	payload, _ := json.Marshal(m.PlatformFacts(context.Background()))
	if bytes.Contains(payload, []byte("private-provider")) || !bytes.Contains(payload, []byte("platform_evidence_unavailable")) {
		t.Fatalf("error projection=%s", payload)
	}
	if got := (*Manager)(nil).PlatformFacts(context.Background()); got.ServiceSupervision.Availability != domain.Unknown {
		t.Fatal("nil provider became available")
	}
}
