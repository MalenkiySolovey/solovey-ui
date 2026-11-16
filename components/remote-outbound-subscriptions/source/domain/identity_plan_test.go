//go:build !minimal

package remote

import (
	"errors"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
	"testing"
)

type identityStoreFixture struct {
	Store
	rows []RemoteOutboundConnection
}

func (s identityStoreFixture) ListRefreshIdentityConnections(_ uint, out *[]RemoteOutboundConnection) error {
	*out = append([]RemoteOutboundConnection(nil), s.rows...)
	return nil
}
func identityEndpoint(label, server, credential string) map[string]any {
	return map[string]any{"type": "vless", "tag": label, "server": server, "server_port": 443, "uuid": credential}
}
func identityConnection(t *testing.T, id uint, raw map[string]any) RemoteOutboundConnection {
	t.Helper()
	n, err := NormalizeOutbound(raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	return RemoteOutboundConnection{Id: id, Name: n.Name, Type: n.Type, SourceKey: n.SourceKey, Options: n.Options, Canonical: n.Canonical}
}
func TestIdentityPlanReservesEndpointsAcrossReorderAndReusedLabels(t *testing.T) {
	old := []RemoteOutboundConnection{identityConnection(t, 10, identityEndpoint("A", "a.example.org", "old")), identityConnection(t, 20, identityEndpoint("B", "b.example.org", "old"))}
	for name, fixture := range map[string]struct {
		raw  []map[string]any
		want map[int]uint
	}{
		"reorder":     {[]map[string]any{identityEndpoint("renamed B", "b.example.org", "old"), identityEndpoint("renamed A", "a.example.org", "old")}, map[int]uint{0: 20, 1: 10}},
		"reuse label": {[]map[string]any{identityEndpoint("A", "new.example.org", "old"), identityEndpoint("old A", "a.example.org", "old"), identityEndpoint("old B", "b.example.org", "old")}, map[int]uint{1: 10, 2: 20}},
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := NewEndpointRefreshPlan(identityStoreFixture{rows: old}, 1, fixture.raw, canonical.Snapshot{})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.matches) != len(fixture.want) {
				t.Fatal("endpoint identity was guessed through a reused label")
			}
			for index, want := range fixture.want {
				if plan.matches[index].Id != want || !plan.unavailable(want, index+10) {
					t.Fatalf("stable identity not reserved: index=%d", index)
				}
			}
		})
	}
}
func TestIdentityPlanRejectsAmbiguousEqualEndpoints(t *testing.T) {
	old := []RemoteOutboundConnection{identityConnection(t, 10, identityEndpoint("A", "same.example.org", "credential")), identityConnection(t, 20, identityEndpoint("B", "same.example.org", "credential"))}
	_, err := NewEndpointRefreshPlan(identityStoreFixture{rows: old}, 1, []map[string]any{identityEndpoint("C", "same.example.org", "credential"), identityEndpoint("D", "same.example.org", "credential")}, canonical.Snapshot{})
	if !errors.Is(err, ErrRefreshIdentityAmbiguous) {
		t.Fatalf("ambiguous mapping accepted: %v", err)
	}
	if old[0].Name != "A" || old[1].Name != "B" {
		t.Fatal("planning mutated durable records")
	}
}
