// Package brokerplugin contributes the component's existing semantic engines
// to the neutral production privileged broker composition seam.
package brokerplugin

import (
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
)

type managedRootConstructor func(protectionruntime.RuntimeRootAuthority) (protectionhelper.ManagedRoot, error)

func bindManagedRoot(authority protectionruntime.RuntimeRootAuthority, constructor managedRootConstructor) (protectionhelper.ManagedRoot, error) {
	return constructor(authority)
}

// RegisterBrokerHandlers is called by the privileged broker composition root
// after it resolves the closed SSH catalog. This keeps the plugin semantic
// owner-local while ensuring recovery receives the same immutable record as
// the broker-facing SSH handlers.
func RegisterBrokerHandlers(registry *broker.Registry, composition sshbroker.ResolvedSSHComposition) error {
	authority, err := protectionruntime.LoadInstalledRuntimeRootAuthority()
	if err != nil {
		return err
	}
	root, err := bindManagedRoot(authority, protectionhelper.NewManagedRoot)
	if err != nil {
		return err
	}
	return protectionhelper.RegisterBrokerHandlersWithSSHComposition(registry, root, composition)
}
