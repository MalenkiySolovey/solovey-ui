package health

import (
	"context"
	"sort"

	componenthealth "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

type Checker interface {
	Check(context.Context, string) componenthealth.Result
}

type SSHChecker interface {
	ResourceKind() string
	Check(context.Context, []hostresources.ProtectableResource) []componenthealth.Result
}

// EvaluateWithSSH retains the SSH semantic owner at the composition seam.
func EvaluateWithSSH(ctx context.Context, resources []hostresources.ProtectableResource, checker Checker, ssh SSHChecker) []componenthealth.Result {
	items := append([]hostresources.ProtectableResource(nil), resources...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	result := make([]componenthealth.Result, 0, len(items))
	if len(items) == 0 {
		return result
	}
	sshKind := ""
	if checker != nil && ssh != nil {
		sshKind = ssh.ResourceKind()
	}
	if sshKind == "" {
		for _, resource := range items {
			result = append(result, componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusMissingCapability, Check: "owner_health", FactCode: "health_owner_unavailable"})
		}
		return result
	}
	var sshResources []hostresources.ProtectableResource
	for _, resource := range items {
		switch resource.Kind {
		case sshKind:
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
