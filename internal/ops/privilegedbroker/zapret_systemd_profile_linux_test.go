//go:build linux && !minimal

package privilegedbroker

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
)

func TestInstalledZapretSecondSocketRollback(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires a disposable root-owned Unix socket directory")
	}
	root, err := os.MkdirTemp("/run", "zapret-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(root) })
	const gid = 65534
	if err := os.Chown(root, 0, gid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o750); err != nil {
		t.Fatal(err)
	}
	first, second := strings.Repeat("1", 64), strings.Repeat("2", 64)
	blocked := zapretSocketPath(root, second)
	if err := os.WriteFile(blocked, []byte("unowned witness"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(blocked) })
	endpoints, err := bindZapretSocketBatch(t.Context(), root, gid, []ZapretSocketGrant{{Generation: first}, {Generation: second}})
	for _, endpoint := range endpoints {
		_ = endpoint.close()
	}
	if err == nil || !strings.Contains(err.Error(), "Zapret socket 2: socket generation path for Zapret is not vacant") || len(endpoints) != 0 {
		t.Fatalf("expected failure only after member 1 bound: %v", err)
	}
	if _, err := os.Lstat(zapretSocketPath(root, first)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("member 1 was not rolled back: %v", err)
	}
	if got, err := os.ReadFile(blocked); err != nil || string(got) != "unowned witness" {
		t.Fatalf("rollback touched the foreign path: %q, %v", got, err)
	}
}

func TestInstalledZapretCatalogAuthority(t *testing.T) {
	root := json.RawMessage(`{"id":"operator"}`)
	const ref = zapretactivation.ManagedStrategyPrefix + "tls"
	managed := json.RawMessage(`{"id":"` + ref + `"}`)
	profile := installedZapretProfile{OutboundRefs: []string{"a", "b"}, Strategies: []json.RawMessage{root}}
	input := zapretactivation.Validation{Bindings: []zapretactivation.Request{
		{OutboundRef: "a", StrategyRef: "operator"}, {OutboundRef: "b", StrategyRef: ref},
	}, Definitions: map[string]json.RawMessage{ref: managed}}
	if _, err := profile.selectStrategies(input); err == nil {
		t.Fatal("editor delegation must be explicit")
	}
	profile.AllowManagedStrategies = true
	got, err := profile.selectStrategies(input)
	if err != nil || string(got[0]) != string(root) || string(got[1]) != string(managed) {
		t.Fatalf("authorized mixed catalog: %s %v", got, err)
	}
	input.Definitions["operator"] = managed
	if _, err := profile.selectStrategies(input); err == nil {
		t.Fatal("attempt to replace a root-owned strategy")
	}
	delete(input.Definitions, "operator")
	input.Bindings[1].OutboundRef = "foreign"
	if _, err := profile.selectStrategies(input); err == nil {
		t.Fatal("editor delegated the outbound allowlist")
	}
	input.Bindings[1].OutboundRef = "b"
	delete(input.Definitions, ref)
	if _, err := profile.selectStrategies(input); err == nil {
		t.Fatal("missing editor strategy admitted")
	}
}
