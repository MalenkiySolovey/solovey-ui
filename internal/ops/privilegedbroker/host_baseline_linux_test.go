//go:build linux && !minimal

package privilegedbroker

import (
	"testing"

	"github.com/google/nftables"
)

func TestHostBaselineObserverIsReadOnly(t *testing.T) {
	registry := NewRegistry()
	if err := RegisterHostBaselineObserver(registry, SystemdActivated); err != nil {
		t.Fatal(err)
	}
	definition, exists := registry.definition(VerbHostBaseline)
	if !exists || definition.Role != RolePanel || definition.Mutation || definition.LocalPathAuthority {
		t.Fatalf("expected a read-only Panel observation: %+v", definition)
	}
	other := NewRegistry()
	if err := RegisterHostBaselineObserver(other, StandaloneOwned); err != nil {
		t.Fatal(err)
	}
	if _, exists := other.definition(VerbHostBaseline); exists {
		t.Fatal("the systemd-only observation was registered on another transport")
	}
}

func TestHostBaselineRejectsUnobservedHooks(t *testing.T) {
	table := &nftables.Table{Family: nftables.TableFamilyINet, Name: "test"}
	input := &nftables.Chain{Table: table, Hooknum: nftables.ChainHookInput, Type: nftables.ChainTypeFilter}
	if !baselineHooksObservable([]*nftables.Chain{input}) {
		t.Fatal("an input filter alone cannot steer the local egress")
	}
	for _, hook := range []*nftables.ChainHook{nftables.ChainHookPrerouting, nftables.ChainHookOutput, nftables.ChainHookPostrouting} {
		other := &nftables.Chain{Table: table, Hooknum: hook, Type: nftables.ChainTypeFilter}
		if baselineHooksObservable([]*nftables.Chain{input, other}) {
			t.Fatalf("an unreviewed hook %v could steer this flow", *hook)
		}
	}
}
