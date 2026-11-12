//go:build !minimal

package fallbackhtml

import (
	"context"
	"errors"
	"reflect"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
)

// The Backend binds the feature's observation port to the existing Health
// registry. It never promotes configured inventory into observed availability.
type targetResourceObserver struct {
	runtime *fallbackservice.Runtime
	health  *componenthealth.Registry
}

func (o targetResourceObserver) Generation() uint64 {
	if o.runtime == nil {
		return 0
	}
	return o.runtime.PublicationState().Generation
}

func (o targetResourceObserver) ObserveTarget(ctx context.Context, request fallbackservice.TargetObservationRequest) (fallbackservice.TargetObservation, error) {
	unknown := fallbackservice.TargetObservation{Availability: hostresources.CapabilityUnknown}
	if ctx == nil {
		return unknown, errors.New("target observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if !request.Resource.Host.IsLocal() || request.Resource.Owner != id || request.Resource.ResourceID == "" || request.ExpectedGeneration == 0 {
		return unknown, errors.New("target observation scope is invalid")
	}
	if o.runtime == nil || o.health == nil {
		return unknown, errors.New("target observation is unavailable")
	}
	before := o.runtime.PublicationState()
	if !reflect.DeepEqual(before.Resource, request.Resource) || before.PublicationRevision != request.PublicationRevision || before.Generation != request.ExpectedGeneration {
		return unknown, errors.New("target observation reference is stale or foreign")
	}
	requirement := componenthealth.Requirement{Owner: id, Source: "fallback-runtime", ProofKind: componenthealth.RuntimeReady,
		DesiredRevision: before.EvidenceRevision(), MaxAge: 30 * time.Second}
	result := o.health.Require(ctx, request.Resource.ResourceID, requirement)
	after := o.runtime.PublicationState()
	if !reflect.DeepEqual(before, after) || ctx.Err() != nil {
		return unknown, errors.New("target observation generation changed")
	}
	unknown.Generation, unknown.Health = before.Generation, result
	if result.Satisfies(requirement, time.Now()) && before.ListenerReady {
		unknown.Availability = hostresources.CapabilityYes
	}
	return unknown, nil
}
