package health

import (
	"context"
	"sort"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	sshdomain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	sshservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
)

type Checker interface {
	Check(context.Context, string) componenthealth.Result
}

type defaultChecker struct{}

func (defaultChecker) Check(ctx context.Context, resourceID string) componenthealth.Result {
	return componenthealth.Check(ctx, resourceID)
}

// Evaluate executes registered runtime checks and current SSH owner authority.
// It does not probe public endpoints, inspect secret paths or persist results.
func Evaluate(ctx context.Context, resources []hostresources.ProtectableResource, checker Checker) []componenthealth.Result {
	return EvaluateWithSSH(ctx, resources, checker, sshservice.ProtectionHealth{Reader: sshservice.Shared()})
}

// EvaluateWithSSH retains the SSH semantic owner at the composition seam.
func EvaluateWithSSH(ctx context.Context, resources []hostresources.ProtectableResource, checker Checker, ssh sshservice.ProtectionHealth) []componenthealth.Result {
	if checker == nil {
		checker = defaultChecker{}
	}
	items := append([]hostresources.ProtectableResource(nil), resources...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	result := make([]componenthealth.Result, 0, len(items))
	var sshResources []hostresources.ProtectableResource
	for _, resource := range items {
		switch resource.Kind {
		case sshdomain.ProtectionResourceKind:
			sshResources = append(sshResources, resource)
		case "inbound":
			result = append(result, componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusMissingCapability, Check: "listener_probe", FactCode: "inbound_listener_probe_unavailable"})
		default:
			result = append(result, checker.Check(ctx, resource.ID))
		}
	}
	if len(sshResources) > 0 {
		result = append(result, ssh.Check(ctx, sshResources)...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ResourceID < result[j].ResourceID })
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
