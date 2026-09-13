package main

import (
	serverprotectionbroker "github.com/MalenkiySolovey/solovey-ui/components/server-protection/brokerplugin"
	deploymentbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/deploymentbroker"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
	updatebroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

// handlerRegistrars is the narrow test seam for the composition root. The
// production value below binds every field to the actual owner registration
// function; tests can prove the complete ordering and selection graph without
// changing absolute production host paths.
type handlerRegistrars struct {
	contributed      func(*broker.Registry) error
	serverProtection func(*broker.Registry, sshbroker.ResolvedSSHComposition) error
	ssh              func(*broker.Registry, sshbroker.ResolvedSSHComposition, broker.CompletedMutationAuthority) error
	deployment       func(*broker.Registry, deploymentbroker.Backend, broker.CompletedMutationAuthority) error
	update           func(*broker.Registry, updatebroker.Mode) error
}

func productionHandlerRegistrars() handlerRegistrars {
	return handlerRegistrars{
		contributed:      broker.RegisterContributedHandlers,
		serverProtection: serverprotectionbroker.RegisterBrokerHandlers,
		ssh:              sshbroker.RegisterHandlersFromResolved,
		deployment:       deploymentbroker.RegisterHandlers,
		update:           updatebroker.RegisterHandlers,
	}
}

func registerHandlerGraph(registry *broker.Registry, composition runtimeComposition, journal broker.CompletedMutationAuthority, registrars handlerRegistrars) error {
	if err := registrars.contributed(registry); err != nil {
		return broker.EnsureStartupFailure(err, "broker", "broker.handlers", "contributed", "registration_failed")
	}
	if err := registrars.serverProtection(registry, composition.ssh); err != nil {
		return broker.EnsureStartupFailure(err, "server-protection", "server_protection.handlers", "installed", "registration_failed")
	}
	if err := registrars.ssh(registry, composition.ssh, journal); err != nil {
		return broker.EnsureStartupFailure(err, "ssh", "ssh.management", string(composition.ssh.Implementation()), "registration_failed")
	}
	if err := registrars.deployment(registry, composition.deploymentBackend, journal); err != nil {
		return broker.EnsureStartupFailure(err, "deployment", "deployment.lifecycle", string(composition.deploymentBackend), "registration_failed")
	}
	if err := registrars.update(registry, composition.updateMode); err != nil {
		return broker.EnsureStartupFailure(err, "update", "update.lifecycle", string(composition.updateMode), "registration_failed")
	}
	return nil
}
