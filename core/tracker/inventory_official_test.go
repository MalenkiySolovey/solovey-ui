//go:build !shtorm_extended

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
	logger := log.NewNOPFactory().Logger()
	manager := outbound.NewManager(logger, outbound.NewRegistry(), endpoint.NewManager(logger, endpoint.NewRegistry()), "")
	manager.Initialize(func() (adapter.Outbound, error) { return fakeStatsOutbound{tag: "default"}, nil })
	if err := manager.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	inventory := trafficcontrol.NewManager(manager)
	if err := inventory.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inventory.Close(); _ = manager.Close() })
	return inventory
}
