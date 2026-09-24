package sshmanagement

import (
	"context"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

// ProtectionHealth consumes a new owner observation for this health invocation.
// It never uses the Prepare projection as evidence of current listener readiness.
// The provider and the domain projection retain ownership of discovery/validation.
type ProtectionHealth struct {
	Reader ProtectionResourceReader
	Now    func() time.Time
}

func (h ProtectionHealth) Check(ctx context.Context, planned []hostresources.ProtectableResource) []componenthealth.Result {
	ctx, cancel := context.WithTimeout(ctx, componenthealth.DefaultTimeout)
	defer cancel()
	// A manager creates a new local read even if the caller carries an older
	// composed baseline context. All resources below share this one generation.
	if manager, ok := h.Reader.(*Manager); ok {
		ctx = manager.WithCurrentPosture(ctx)
	}
	current, err := ProtectionResourceContributor(h).ListProtectableResources(ctx)
	results := make([]componenthealth.Result, 0, len(planned))
	for _, resource := range planned {
		result := componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusDegraded, Check: "ssh_listener_authority", FactCode: "ssh_listener_authority_unavailable"}
		if err == nil && ctx.Err() == nil {
			result.FactCode = "ssh_management_endpoint_changed"
			for _, live := range current {
				if domain.SameProtectionEndpoint(resource, live) {
					result.Status, result.FactCode = componenthealth.StatusOK, "ssh_management_listener_current"
					break
				}
			}
		}
		results = append(results, result)
	}
	return results
}
