//go:build !minimal

package main

import (
	serverprotectionbroker "github.com/MalenkiySolovey/solovey-ui/components/server-protection/brokerplugin"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
)

// Full-profile broker builds resolve optional semantic handlers through the
// manifest-generated composition seam. Missing generation must fail closed.
var _ = soloveyGeneratedPrivilegedBrokerImports

func registerServerProtection(registry *broker.Registry, composition sshbroker.ResolvedSSHComposition) error {
	return serverprotectionbroker.RegisterBrokerHandlers(registry, composition)
}
