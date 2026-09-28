//go:build minimal

package main

import (
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	sshbroker "github.com/MalenkiySolovey/solovey-ui/internal/ops/sshbroker"
)

func TestCoreBrokerDoesNotRequireOptionalInstalledAuthority(t *testing.T) {
	registry := broker.NewRegistry()
	composition, err := sshbroker.ResolveRegisteredSSHComposition(sshbroker.SystemdOpenSSHComposition())
	if err != nil {
		t.Fatal(err)
	}
	if err := productionHandlerRegistrars().serverProtection(registry, composition); err != nil {
		t.Fatal(err)
	}
	if len(registry.Verbs(broker.RolePanel)) != 0 {
		t.Fatal("core broker registered optional protection verbs")
	}
}
