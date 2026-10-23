//go:build shtorm_extended

package tracker

import (
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing-box/log"
)

func newTestInventory(t *testing.T) *trafficcontrol.Manager {
	t.Helper()
	scope := adapter.NewScope(t.Context(), log.NewNOPFactory().Logger())
	t.Cleanup(func() { _ = scope.Close() })
	manager := outbound.NewManager(outbound.NewRegistry(), endpoint.NewManager(endpoint.NewRegistry()), "")
	manager.Initialize(func() (adapter.Outbound, error) { return fakeStatsOutbound{tag: "default"}, nil })
	if err := scope.Start("outbound", manager, adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	inventory := trafficcontrol.NewManager(manager)
	if err := scope.Start("inventory", inventory, adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	return inventory
}
