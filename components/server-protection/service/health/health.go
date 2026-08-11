package health

import (
	"context"
	"sort"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type Checker interface {
	Check(context.Context, string) componenthealth.Result
}

type defaultChecker struct{}

func (defaultChecker) Check(ctx context.Context, resourceID string) componenthealth.Result {
	return componenthealth.Check(ctx, resourceID)
}

// Evaluate only executes owner-provided, in-process checks. It does not dial
// public endpoints, inspect secret paths, emit observations or persist results.
func Evaluate(ctx context.Context, resources []hostresources.ProtectableResource, checker Checker) []componenthealth.Result {
	if checker == nil {
		checker = defaultChecker{}
	}
	items := append([]hostresources.ProtectableResource(nil), resources...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	result := make([]componenthealth.Result, 0, len(items))
	for _, resource := range items {
		switch resource.Kind {
		case "panel_web", "subscription", "public_site":
			result = append(result, checker.Check(ctx, resource.ID))
		case "inbound":
			result = append(result, componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusMissingCapability, Check: "listener_probe", FactCode: "inbound_listener_probe_unavailable"})
		}
	}
	return result
}

func Summary(results []componenthealth.Result) componenthealth.Status {
	status := componenthealth.StatusOK
	for _, result := range results {
		if result.Status == componenthealth.StatusMissingCapability {
			return componenthealth.StatusMissingCapability
		}
		if result.Status == componenthealth.StatusDegraded {
			status = componenthealth.StatusDegraded
		}
	}
	return status
}
