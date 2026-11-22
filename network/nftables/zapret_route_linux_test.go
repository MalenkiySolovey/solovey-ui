//go:build linux && !minimal && zapretproof

package nftables

import (
	"net"
	"net/netip"
	"os"
	"os/exec"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

func TestZapretLinuxGatewaySnapshot(t *testing.T) {
	parent := os.Getenv("SOLOVEY_TEST_PARENT_NETNS")
	if parent == "" {
		t.Skip("requires a disposable network namespace")
	}
	if _, err := probeNamespaceForGuard(parent); err != nil {
		t.Fatal(err)
	}
	ip := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "ip", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ip %v: %v\n%s", args, err, out)
		}
	}
	const link = "r8broute0"
	ip("link", "add", link, "type", "veth", "peer", "name", "r8broute1")
	ip("link", "set", link, "up")
	ip("link", "set", "r8broute1", "up")
	ip("addr", "add", "192.0.2.2/24", "dev", link)
	ip("-6", "addr", "add", "2001:db8::2/64", "dev", link, "nodad")
	dev, err := net.InterfaceByName(link)
	if err != nil {
		t.Fatal(err)
	}
	index := int32(dev.Index)
	if _, err := planner.ObserveZapretLinuxEgress(t.Context(), index, true, true); err == nil {
		t.Fatal("missing default routes were accepted")
	}
	ip("-4", "route", "add", "default", "via", "192.0.2.1", "dev", link)
	ip("-6", "route", "add", "default", "via", "fe80::1", "dev", link)
	read := func() planner.ZapretEgress {
		t.Helper()
		e, err := planner.ObserveZapretLinuxEgress(t.Context(), index, true, true)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	first := read()
	if first.IfIndex != index || first.NamespaceCookie == 0 || len(first.RouteRevision) != 64 ||
		first.IPv4Gateway != netip.MustParseAddr("192.0.2.1") || first.IPv6Gateway != netip.MustParseAddr("fe80::1") || read() != first {
		t.Fatalf("unstable or incorrect dual-stack route snapshot: %+v", first)
	}
	ip("-4", "route", "replace", "default", "via", "192.0.2.3", "dev", link)
	second := read()
	if second.IfIndex != first.IfIndex || second.NamespaceCookie != first.NamespaceCookie || second.RouteRevision == first.RouteRevision ||
		second.IPv4Gateway != netip.MustParseAddr("192.0.2.3") || second.IPv6Gateway != first.IPv6Gateway {
		t.Fatalf("same-link gateway change was not observed: %+v", second)
	}
	ip("-4", "rule", "add", "priority", "1050", "fwmark", "0/0x100", "lookup", "200")
	ip("-6", "rule", "add", "priority", "1050", "fwmark", "0x400/0xc00", "lookup", "200")
	policy := read()
	if policy.IPv4Gateway != second.IPv4Gateway || policy.IPv6Gateway != second.IPv6Gateway || policy.RouteRevision == second.RouteRevision || policy.RoutingMarkBits != 0xd00 {
		t.Fatalf("masked rule inventory was incomplete: %+v", policy)
	}
	if v4, err := planner.ObserveZapretLinuxEgress(t.Context(), index, true, false); err != nil || v4.RoutingMarkBits != policy.RoutingMarkBits || v4.IPv6Gateway.IsValid() {
		t.Fatalf("IPv4-only path missed IPv6 policy selectors: %+v (%v)", v4, err)
	}
	ip("-4", "rule", "del", "priority", "1050")
	ip("-6", "rule", "del", "priority", "1050")
	if restored := read(); restored != second {
		t.Fatalf("routing policy did not return to the original identity: %+v", restored)
	}
	ip("-4", "route", "add", "default", "via", "192.0.2.1", "dev", link, "metric", "100")
	if _, err := planner.ObserveZapretLinuxEgress(t.Context(), index, true, false); err == nil {
		t.Fatal("multiple default routes were accepted")
	}
	t.Log("KERNEL_GATEWAY_SNAPSHOT=PASS (IPv4 and IPv6, same-link change, missing and ambiguous default refused)")
	t.Log("KERNEL_ROUTING_MARK_SELECTORS=PASS (masked IPv4/IPv6 rules, including zero mark)")
}
