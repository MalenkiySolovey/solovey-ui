// Package backendhealth binds protection Health orchestration to the existing
// registered checks and current SSH owner. It creates no probe or registry.
package backendhealth

import (
	"context"

	componenthealth "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	registered "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	feature "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/health"
	sshservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
)

type registeredChecks struct{}

func (registeredChecks) Check(ctx context.Context, resourceID string) componenthealth.Result {
	return registered.Check(ctx, resourceID)
}

func Evaluate(ctx context.Context, resources []hostresources.ProtectableResource, checker feature.Checker) []componenthealth.Result {
	if checker == nil {
		checker = registeredChecks{}
	}
	return feature.EvaluateWithSSH(ctx, resources, checker, sshservice.ProtectionHealth{Reader: sshservice.Shared()})
}

func Summary(results []componenthealth.Result) componenthealth.Status {
	return feature.Summary(results)
}
