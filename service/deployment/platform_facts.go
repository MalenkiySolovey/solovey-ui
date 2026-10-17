package deployment

import (
	"context"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

// PlatformFacts observes through the existing provider without persisting
// desired state or invoking lifecycle methods. Busy/cancelled/unavailable reads
// are explicit unknowns. The trusted providers receive a bounded read context;
// no new worker, fallback probe or privileged mechanism is introduced.
func (m *Manager) PlatformFacts(ctx context.Context) domain.PlatformFacts {
	now := time.Now()
	if m != nil {
		now = m.now()
	}
	if ctx.Err() != nil {
		return domain.UnknownPlatformFacts("platform_observation_cancelled", now)
	}
	if m == nil || m.Provider == nil {
		return domain.UnknownPlatformFacts("platform_provider_unavailable", now)
	}
	if !m.observeMu.TryLock() {
		return domain.UnknownPlatformFacts("platform_observation_busy", now)
	}
	defer m.observeMu.Unlock()
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	posture, err := m.Provider.Observe(readCtx)
	if readCtx.Err() != nil {
		return domain.UnknownPlatformFacts("platform_observation_cancelled", m.now())
	}
	if err != nil {
		return domain.UnknownPlatformFacts("platform_evidence_unavailable", m.now())
	}
	facts := domain.ProjectPlatformFacts(posture, m.Provider.ProviderID(), m.now())
	// Reuse portable Update's existing semantic owner contract. A package-owned
	// lifecycle does not imply an observed apk/opkg backend or a web package verb.
	if facts.Source != "unavailable" {
		if owner, ok := m.Provider.(updateLifecycleProvider); ok {
			switch lifecycle := owner.UpdateLifecycle(); lifecycle {
			case UpdateLifecycleSelfManaged, UpdateLifecyclePackageManaged, UpdateLifecycleOperatorManaged:
				facts.UpdateLifecycle = domain.PlatformFact{Availability: domain.Available, Backend: string(lifecycle),
					Reason: "update_lifecycle_owner_not_operation_authority"}
			case UpdateLifecycleUnavailable:
				facts.UpdateLifecycle = domain.PlatformFact{Availability: domain.Unavailable, Backend: "unavailable",
					Reason: "update_lifecycle_unavailable"}
			}
		}
		facts.Revision = ""
		facts.Revision = domain.Revision(facts)
	}
	return facts
}
