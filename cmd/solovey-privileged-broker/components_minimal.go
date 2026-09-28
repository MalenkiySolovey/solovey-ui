//go:build minimal

package main

import (
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
)

// Core delivery has no optional Server Protection authority or handlers.
// Installed full delivery still fails closed on any invalid authority.
func registerServerProtection(*broker.Registry, sshbroker.ResolvedSSHComposition) error { return nil }
