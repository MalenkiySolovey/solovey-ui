// Package backendfacts binds protection readers to the existing fact owners.
package backendfacts

import (
	"context"
	"time"

	contractfacts "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
	contractresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
)

type inventoryReader struct{}

func (inventoryReader) Snapshot(ctx context.Context) contractresources.ResourceSnapshot {
	return resources.Snapshot(ctx)
}
func (inventoryReader) Refresh(ctx context.Context) contractresources.ResourceSnapshot {
	return resources.Refresh(ctx)
}

type surfaceReader struct{}

func (surfaceReader) Snapshot() contractfacts.Snapshot { return hostsurface.CurrentSnapshot() }
func (surfaceReader) Reconcile(ctx context.Context) contractfacts.Snapshot {
	return hostsurface.Reconcile(ctx)
}

func BaselineSources() firewall.BaselineSources {
	return firewall.BaselineSources{Inventory: inventoryReader{}, Surfaces: surfaceReader{}, Endpoints: management.Endpoints, EffectiveRecovery: management.Effective,
		Targets: func(ctx context.Context, now time.Time) []firewall.BaselineTargetFact {
			snapshot := fallbacktargets.Default.Snapshot(ctx, now)
			result := make([]firewall.BaselineTargetFact, 0, len(snapshot.Targets))
			for _, target := range snapshot.Targets {
				result = append(result, firewall.BaselineTargetFact{ProviderID: target.Identity.ProviderID, TargetID: target.Identity.TargetID, EndpointID: target.Endpoint.EndpointID, PublishRevision: target.PublishRevision, ProviderHealthRevision: target.ProviderHealthRevision, ContentDigest: target.ContentDigest, Readiness: target.Readiness})
			}
			return result
		}, RecoveryEvidence: func(ctx context.Context, now time.Time) firewall.BaselineRecoveryEvidence {
			snapshot := management.RecoveryEvidence(ctx, now)
			return firewall.BaselineRecoveryEvidence{Paths: snapshot.Paths, ReasonCodes: snapshot.ReasonCodes}
		},
	}
}
