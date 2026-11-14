package firewall

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

// These integration cases retain the same actual registry, SSH projection and
// management owners used before constructor injection.
func baselineSourcesFixture() BaselineSources {
	return BaselineSources{Inventory: resources.Default, Surfaces: hostsurface.Default, Endpoints: management.Endpoints, EffectiveRecovery: management.Effective,
		Targets: func(ctx context.Context, now time.Time) []BaselineTargetFact {
			snapshot := fallbacktargets.Default.Snapshot(ctx, now)
			result := make([]BaselineTargetFact, 0, len(snapshot.Targets))
			for _, target := range snapshot.Targets {
				result = append(result, BaselineTargetFact{ProviderID: target.Identity.ProviderID, TargetID: target.Identity.TargetID, EndpointID: target.Endpoint.EndpointID, PublishRevision: target.PublishRevision, ProviderHealthRevision: target.ProviderHealthRevision, ContentDigest: target.ContentDigest, Readiness: target.Readiness})
			}
			return result
		}, RecoveryEvidence: func(ctx context.Context, now time.Time) BaselineRecoveryEvidence {
			snapshot := management.RecoveryEvidence(ctx, now)
			return BaselineRecoveryEvidence{Paths: snapshot.Paths, ReasonCodes: snapshot.ReasonCodes}
		},
	}
}
