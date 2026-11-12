//go:build !minimal

package service

import (
	"context"
	"fmt"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
)

// TargetObservationRequest binds a read to the configured owner resource and
// the runtime generation seen by its caller. It cannot acquire or release a
// listener or change a durable reservation.
type TargetObservationRequest struct {
	Resource            hostresources.ResourceIdentityV1
	PublicationRevision string
	ExpectedGeneration  uint64
}

type TargetObservation struct {
	Generation   uint64
	Availability hostresources.CapabilityValue
	Health       componenthealth.Result
}

type TargetObserver interface {
	Generation() uint64
	ObserveTarget(context.Context, TargetObservationRequest) (TargetObservation, error)
}

// PublicationState is a bounded projection of the active immutable snapshot.
// Configured identity remains UNKNOWN observation; only the Health owner can
// provide evidence for this exact publication and runtime generation.
type PublicationState struct {
	Resource            hostresources.ResourceIdentityV1
	PublicationRevision string
	Generation          uint64
	ListenerReady       bool
	ListenerRequired    bool
	Draining            bool
}

func (s PublicationState) EvidenceRevision() string {
	return hostresources.Revision(struct {
		Resource   hostresources.ResourceIdentityV1
		Publish    string
		Generation uint64
	}{s.Resource, s.PublicationRevision, s.Generation})
}

func ConfiguredTargetIdentity(siteID uint, target fallbackdomain.RuntimeTarget) hostresources.ResourceIdentityV1 {
	normalized := hostresources.NormalizeListen(target.Listen)
	revision := hostresources.Revision(struct {
		SiteID    uint
		TargetID  uint
		Kind      string
		Listen    string
		Port      int
		TLS       bool
		UpdatedAt int64
	}{siteID, target.ID, target.Kind, normalized.Value, target.Port, target.TLS, target.UpdatedAt})
	resource := hostresources.ProtectableResource{
		ID: fmt.Sprintf("component:fallback-html:site:%d", siteID), Owner: "fallback-html", Source: "component",
		Capabilities: hostresources.ProtectableResourceCapabilities{ConfigRevision: revision},
	}
	return *hostresources.LocalConfiguredIdentity(resource)
}

func (r *Runtime) PublicationState() PublicationState {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := r.snapshot.Load().(*snapshot)
	if snap == nil {
		return PublicationState{}
	}
	state := snap.state
	state.Draining = r.draining.Load()
	state.ListenerRequired = snap.targetKey != ""
	if snap.targetKey != "" {
		state.ListenerReady = !r.draining.Load() && r.serving.OwnsKey(snap.targetKey)
	}
	return state
}
