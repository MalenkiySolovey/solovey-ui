//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

type testNativeTrace struct{ io.Writer }

func (t testNativeTrace) MatchedProfiles(binding int) []string {
	if binding == 0 {
		return []string{"http-a"}
	}
	return nil
}

func TestInstalledZapretObserveReturnsOnlyVerifiedGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	peer := PeerIdentity{UID: 1000, Revision: strings.Repeat("1", 64)}
	id := strings.Repeat("2", 64)
	input, err := json.Marshal(zapretactivation.BatchIdentity{BatchID: id})
	if err != nil {
		t.Fatal(err)
	}
	owner := installedZapret{active: &installedZapretPath{
		ctx: ctx, peer: peer.Revision, uid: peer.UID, queue: 401, trace: testNativeTrace{io.Discard},
		grant: zapretactivation.BatchGrant{BatchID: id, Bindings: []zapretactivation.Grant{{
			Request:          zapretactivation.Request{OutboundRef: "a", StrategyRef: "strategy"},
			StrategyRevision: strings.Repeat("3", 64),
		}, {
			Request:          zapretactivation.Request{OutboundRef: "b", StrategyRef: "another"},
			StrategyRevision: strings.Repeat("4", 64),
		}}},
		verify: func(context.Context) error { return nil },
	}}
	value, err := owner.observe(ctx, Request{Payload: input}, peer)
	got, ok := value.([]zapretactivation.AppliedBinding)
	if err != nil || !ok || len(got) != 2 || got[0].Queue != 401 || got[0].OutboundRef != "a" ||
		got[0].NativeMatches == nil || !slices.Equal(got[0].NativeMatches.Profiles, []string{"http-a"}) ||
		got[1].OutboundRef != "b" || got[1].NativeMatches == nil || len(got[1].NativeMatches.Profiles) != 0 {
		t.Fatalf("wrong active queue or profile observations: %+v, %v", value, err)
	}
	owner.active.grant.BatchID = strings.Repeat("5", 64)
	if value, err := owner.observe(ctx, Request{Payload: input}, peer); err == nil || value != nil {
		t.Fatalf("retired generation returned profile observations: %+v, %v", value, err)
	}
	owner.active.grant.BatchID = id
	owner.active.verify = func(context.Context) error { return errZapretSocket }
	if value, err := owner.observe(ctx, Request{Payload: input}, peer); err == nil || value != nil {
		t.Fatalf("observation reported an unverified queue: %+v, %v", value, err)
	}
}

func TestInstalledZapretDNSRequiresFamilyAndEffectiveQUICProfile(t *testing.T) {
	strategies := InstalledZapretStrategies{
		Families: []planner.ZapretFamilies{{
			IPv4: planner.ZapretPorts{TCP: []uint16{443}, UDP: []uint16{443}},
			IPv6: planner.ZapretPorts{TCP: []uint16{443}, UDP: []uint16{443}},
		}},
		HandlesQUIC: func(binding int, ipv6 bool, port uint16) bool { return binding == 0 && ipv6 && port == 443 },
	}
	for _, tc := range []struct {
		family              zapretactivation.DNSFamily
		ipv6, quic, allowed bool
	}{
		{zapretactivation.DNSFamilyIPv4, true, false, true},
		{zapretactivation.DNSFamilyIPv4, true, true, false},
		{zapretactivation.DNSFamilyIPv6, true, true, true},
		{zapretactivation.DNSFamilyIPv6, false, true, false},
		{zapretactivation.DNSFamilyIPv6, false, false, false},
		{zapretactivation.DNSFamilyFromProfile, true, true, false},
		{zapretactivation.DNSFamilyFromProfile, true, false, true},
	} {
		need := zapretactivation.DNSPathRequirement{Family: tc.family, Port: 443, QUIC: tc.quic}
		if strategies.handlesDNS(0, tc.ipv6, need) != tc.allowed {
			t.Errorf("incorrect protected DNS coverage for %+v, ipv6=%t", need, tc.ipv6)
		}
	}
}

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
