package update

import (
	"context"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/preflight"
)

type PlatformPreflightError struct{ Report preflight.Report }

func (PlatformPreflightError) Error() string { return "update platform preflight failed" }

func (m *LifecycleManager) platformPreflight(ctx context.Context) preflight.Report {
	facts := domain.UnknownPlatformFacts("platform_provider_unavailable", m.now())
	if m.platformFacts != nil {
		facts = m.platformFacts(ctx)
	}
	return preflight.Platform(ctx, facts, m.now(),
		preflight.Requirement{Dimension: preflight.Supervision},
		preflight.Requirement{Dimension: preflight.Privilege},
		preflight.Requirement{Dimension: preflight.UpdateLifecycle, Backend: "self-managed"})
}
