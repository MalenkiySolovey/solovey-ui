package firewall

import (
	"context"
	"github.com/MalenkiySolovey/solovey-host/componenthost/fallbacktargets"
	hostfacts "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"time"
)

type SurfaceSource interface {
	Snapshot() hostfacts.Snapshot
	Reconcile(context.Context) hostfacts.Snapshot
}

// BaselineTargetFact contains the existing target facts consumed by baseline
// policy. Publication, target inventory and readiness stay with their owners.
type BaselineTargetFact struct {
	ProviderID, TargetID, EndpointID                       string
	PublishRevision, ProviderHealthRevision, ContentDigest string
	Readiness                                              fallbacktargets.Readiness
}

type BaselineRecoveryEvidence struct {
	Paths       []hostresources.RecoveryPathV1
	ReasonCodes []string
}

// BaselineSources supplies only the existing bounded observations and recovery
// admission consumed by baseline policy. It grants no registry mutation.
type BaselineSources struct {
	Inventory         protectionresources.InventoryReader
	Surfaces          SurfaceSource
	Targets           func(context.Context, time.Time) []BaselineTargetFact
	Endpoints         func([]hostresources.ProtectableResource, hostfacts.Snapshot, time.Time) []hostresources.ManagementEndpointV1
	RecoveryEvidence  func(context.Context, time.Time) BaselineRecoveryEvidence
	EffectiveRecovery func(hostresources.RecoveryPathV1, []hostresources.ManagementEndpointV1, time.Time) hostresources.RecoveryPathV1
}

func (s BaselineSources) available() bool {
	return s.Inventory != nil && s.Surfaces != nil && s.Targets != nil && s.Endpoints != nil && s.RecoveryEvidence != nil && s.EffectiveRecovery != nil
}
