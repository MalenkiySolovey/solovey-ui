package planner

import (
	"errors"
	"fmt"
	"net/netip"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func zapretFixture() (intent.ZapretPacketPathIntent, Facts) {
	value := intent.ZapretPacketPathIntent{
		Schema: intent.ZapretPacketPathSchema, HostRef: hostref.Local(), Owner: intent.ZapretOwner,
		ResourceRef: "outbound:direct-a", OutboundRef: "direct-a", StrategyRef: "strategy-a",
		StrategyRevision: hash("strategy"), DesiredRevision: hash("strategy"), InputRevision: hash("outbound"),
		Protocols: []intent.PacketProtocol{intent.PacketProtocolTCP}, TCPPorts: []uint16{443},
		Directions: []intent.PacketDirection{intent.PacketDirectionOutbound}, Purpose: intent.ZapretPurpose,
	}
	facts := Facts{
		HostRef: hostref.Local(), Revision: hash("observation"), CapabilityRevision: hash("adapter"),
		Capability:        platform.PlatformFact{Availability: platform.Available, Backend: ZapretNamespaceBackend, Reason: "isolated"},
		InventoryComplete: true,
		Zapret: &ZapretFacts{OutboundRef: "direct-a", OutboundRevision: hash("outbound"), Provider: "sing-box",
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true},
	}
	return value, facts
}

func TestCompileZapretAllocatesFreshBoundedResources(t *testing.T) {
	request, facts := zapretFixture()
	first, err := CompileZapret(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileZapret(request, facts)
	if err != nil || first.Queue() != second.Queue() || first.SocketMark() != second.SocketMark() || first.LoopMark() != second.LoopMark() || first.QueueMark() != second.QueueMark() {
		t.Fatalf("allocation is not deterministic: first=%#v second=%#v err=%v", first, second, err)
	}
	if first.Queue() == 0 || first.SocketMark() == 0 || first.LoopMark() == 0 || first.SocketMark() == first.LoopMark() {
		t.Fatalf("invalid allocation: queue=%d mark=%x loop=%x", first.Queue(), first.SocketMark(), first.LoopMark())
	}
	pass := first.QueueMark() ^ first.SocketMark()
	if pass == 0 || pass&(pass-1) != 0 || first.QueueMark()&first.SocketMark() == 0 || first.QueueMark()&first.LoopMark() != 0 {
		t.Fatalf("invalid packet admission mark: socket=%x queue=%x loop=%x", first.SocketMark(), first.QueueMark(), first.LoopMark())
	}
	if len(first.Plan().Resources()) != 6 {
		t.Fatalf("unexpected owned resources: %#v", first.Plan().Resources())
	}
	accepted, err := Accept(first.Plan(), facts, hash("candidate"))
	if err != nil {
		t.Fatal(err)
	}
	requireReason(t, CheckAccepted(accepted, facts, hash("other bytes")), CandidateChanged)
	changed := clone(facts)
	changed.Zapret.OccupiedQueues = []uint16{first.Queue()}
	requireReason(t, CheckAccepted(accepted, changed, hash("candidate")), StalePlan)
	other, err := CompileZapret(request, changed)
	if err != nil || other.Queue() == first.Queue() {
		t.Fatalf("occupied queue was reused: %d %v", other.Queue(), err)
	}
}

func TestZapretSystemdBatchSharesOnlyQueueAndEgress(t *testing.T) {
	a, first := zapretFixture()
	first.Capability.Backend = ZapretSystemdBackend
	first.Zapret.InstalledEgress = &ZapretEgress{NamespaceCookie: 11, IfIndex: 2, IPv4Gateway: netip.MustParseAddr("192.0.2.1"), RouteRevision: hash("egress")}
	first.Zapret.SystemdSerial = 15
	first.Zapret.OccupiedQueues = []uint16{8, 17}
	b := a
	b.OutboundRef, b.ResourceRef, b.InputRevision = "direct-b", "outbound:direct-b", hash("outbound-b")
	second := clone(first)
	second.Zapret.OutboundRef, second.Zapret.OutboundRevision, second.Zapret.SystemdSerial = b.OutboundRef, b.InputRevision, 16
	requests, facts := []intent.ZapretPacketPathIntent{a, b}, []Facts{first, second}
	plans, err := CompileZapretSystemdBatch(requests, facts)
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].Queue() != plans[1].Queue() || plans[0].LoopMark() != plans[1].LoopMark() ||
		plans[0].SocketMark() == plans[1].SocketMark() || plans[0].QueueMark() == plans[1].QueueMark() {
		t.Fatal("members do not have disjoint selectors on one queue")
	}
	facts[1].Zapret.SystemdSerial = 15
	if batch, err := CompileZapretSystemdBatch(requests, facts); err == nil || batch != nil {
		t.Fatal("planner accepted repeated generation mark in a shared queue")
	}
	facts[1].Zapret.SystemdSerial = 16
	facts[1].Zapret.InstalledEgress.IfIndex = 3
	if batch, err := CompileZapretSystemdBatch(requests, facts); err == nil || batch != nil {
		t.Fatal("planner accepted inconsistent egress in a shared queue")
	}
}

func TestZapretSystemdBatchAllocatesMoreThanThreeDistinctBindings(t *testing.T) {
	request, observation := zapretFixture()
	observation.Capability.Backend = ZapretSystemdBackend
	observation.Zapret.InstalledEgress = &ZapretEgress{NamespaceCookie: 11, IfIndex: 2,
		IPv4Gateway: netip.MustParseAddr("192.0.2.1"), RouteRevision: hash("egress")}
	requests, facts := make([]intent.ZapretPacketPathIntent, 6), make([]Facts, 6)
	for i := range requests {
		ref := fmt.Sprintf("selected-%d", i)
		value, observed := request, clone(observation)
		value.ResourceRef, value.OutboundRef, value.InputRevision = "outbound:"+ref, ref, hash(ref)
		observed.Zapret.OutboundRef, observed.Zapret.OutboundRevision = ref, value.InputRevision
		observed.Zapret.SystemdSerial = uint32(i + 1)
		requests[i], facts[i] = value, observed
	}
	plans, err := CompileZapretSystemdBatch(requests, facts)
	if err != nil || len(plans) != len(requests) {
		t.Fatalf("resource-compatible members were not allocated together: %v", err)
	}
	seen := make(map[uint32]bool, len(plans))
	for _, plan := range plans {
		if plan.Queue() == 0 || plan.Queue() != plans[0].Queue() || seen[plan.SocketMark()] {
			t.Fatal("allocated separate queues or reused a mark")
		}
		seen[plan.SocketMark()] = true
	}
}

func TestZapretSystemdScopesUDPAndIPv6ByFamily(t *testing.T) {
	request, facts := zapretFixture()
	request.Protocols = []intent.PacketProtocol{intent.PacketProtocolTCP, intent.PacketProtocolUDP}
	request.TCPPorts, request.UDPPorts = []uint16{443}, []uint16{18443}
	facts.Capability.Backend = ZapretSystemdBackend
	facts.Zapret.SystemdSerial = 1
	facts.Zapret.InstalledEgress = &ZapretEgress{NamespaceCookie: 11, IfIndex: 2,
		IPv4Gateway: netip.MustParseAddr("192.0.2.1"), RouteRevision: hash("egress")}
	facts.Zapret.SystemdFamilies = &ZapretFamilies{
		IPv4: ZapretPorts{TCP: []uint16{443}}, IPv6: ZapretPorts{UDP: []uint16{18443}}, RequiresIPv6: true,
	}
	_, err := CompileZapret(request, facts)
	requireReason(t, err, CapabilityUnavailable)
	facts.Zapret.InstalledEgress.IPv6Gateway = netip.MustParseAddr("2001:db8::1")
	plan, err := CompileZapret(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := plan.SocketScope()
	if err != nil || !scope.AllowsFamily(intent.PacketProtocolTCP, false) || !scope.AllowsFamily(intent.PacketProtocolUDP, true) ||
		scope.AllowsFamily(intent.PacketProtocolUDP, false) || scope.AllowsFamily(intent.PacketProtocolTCP, true) {
		t.Fatal("protocol admission crossed families", err)
	}
	facts.Zapret.SystemdFamilies.IPv6.UDP[0] = 18444
	if ports := plan.PortsForFamily(intent.PacketProtocolUDP, true); len(ports) != 1 || ports[0] != 18443 {
		t.Fatal("caller mutated admitted ports", ports)
	}
	_, err = CompileZapret(request, facts)
	requireReason(t, err, InvalidIntent)
	facts.Zapret.SystemdFamilies = nil
	_, err = CompileZapret(request, facts)
	requireReason(t, err, InvalidIntent)
}

func TestInstalledZapretScopeBindsEgressAndRejectsProbe(t *testing.T) {
	request, facts := zapretFixture()
	probe, err := CompileZapret(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	_, err = probe.SocketScope()
	requireReason(t, err, CapabilityUnavailable)
	facts.Capability.Backend = ZapretInstalledBackend
	_, err = CompileZapret(request, facts)
	requireReason(t, err, CapabilityUnavailable)
	facts.Zapret.InstalledEgress = &ZapretEgress{NamespaceCookie: 11, IfIndex: 2, IPv4Gateway: netip.MustParseAddr("192.0.2.1"), RouteRevision: hash("egress")}
	plan, err := CompileZapret(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := plan.SocketScope()
	if err != nil || !scope.Valid() || scope.Mark() != plan.SocketMark() || scope.PlanRevision() != plan.Plan().Revision() ||
		scope.OutboundRef() != request.OutboundRef || scope.StrategyRevision() != request.StrategyRevision ||
		!scope.AllowsProtocol(intent.PacketProtocolTCP) || scope.AllowsProtocol(intent.PacketProtocolUDP) {
		t.Fatalf("wrong installed scope: %#v (%v)", scope, err)
	}
	facts.Zapret.InstalledEgress.IfIndex = 3
	if scope.Egress().IfIndex != 2 {
		t.Fatal("observed egress mutation changed existing allocation")
	}
	requireReason(t, CheckCurrent(plan.Plan(), facts), StalePlan)
	facts.Zapret.InstalledEgress.IfIndex = 2
	facts.Zapret.InstalledEgress.IPv4Gateway = netip.MustParseAddr("192.0.2.2")
	requireReason(t, CheckCurrent(plan.Plan(), facts), StalePlan)
	facts.Zapret.InstalledEgress.IPv4Gateway = netip.MustParseAddr("192.0.2.1")
	facts.Zapret.InstalledEgress.RouteRevision = hash("new route, same link")
	requireReason(t, CheckCurrent(plan.Plan(), facts), StalePlan)
	facts.Zapret.InstalledEgress.IPv4Gateway = netip.MustParseAddr("::ffff:192.0.2.1")
	_, err = CompileZapret(request, facts)
	requireReason(t, err, CapabilityUnavailable)
}

func TestCompileZapretFailsClosedForUnsupportedPathAndCollision(t *testing.T) {
	for name, edit := range map[string]func(*intent.ZapretPacketPathIntent, *Facts){
		"tun auto redirect": func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.TUNAutoRedirect = true },
		"no accounting":     func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.ConntrackAccounting = false },
		"no guard":          func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.EgressGuard = false },
		"mark collision":    func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.ConfiguredSocketMark = 1 },
		"only two marks":    func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.ReservedMarkBits = ^uint32(3) },
		"foreign guard": func(_ *intent.ZapretPacketPathIntent, f *Facts) {
			f.Allocations = []Allocation{{Ownership: Ownership{Allocation: ZapretProbeGuard}}}
		},
		"foreign table": func(_ *intent.ZapretPacketPathIntent, f *Facts) {
			f.Allocations = []Allocation{{Ownership: Ownership{Allocation: "inet:solovey_zapret_probe"}}}
		},
		"return capture": func(i *intent.ZapretPacketPathIntent, _ *Facts) {
			i.NeedsReturn = true
			i.Directions = append(i.Directions, intent.PacketDirectionInbound)
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, current := zapretFixture()
			edit(&candidate, &current)
			_, err := CompileZapret(candidate, current)
			if err == nil {
				t.Fatal("unsupported or colliding packet path was accepted")
			}
			var conflict *Conflict
			if !errors.As(err, &conflict) {
				t.Fatalf("untyped planner error: %v", err)
			}
		})
	}
}

func TestInstalledZapretDoesNotReuseRoutingMarkSelectors(t *testing.T) {
	request, facts := zapretFixture()
	facts.Capability.Backend = ZapretInstalledBackend
	facts.Zapret.ReservedMarkBits = ^uint32(0x3f)
	facts.Zapret.InstalledEgress = &ZapretEgress{
		NamespaceCookie: 11, IfIndex: 2, IPv4Gateway: netip.MustParseAddr("192.0.2.1"),
		RouteRevision: hash("egress"), RoutingMarkBits: 3,
	}
	plan, err := CompileZapret(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	if bits := plan.PacketMarkMask(); bits != 0x3c {
		t.Fatalf("expected only the four unclaimed bits, got %#x", bits)
	}
	facts.Zapret.InstalledEgress.RoutingMarkBits |= 4
	_, err = CompileZapret(request, facts)
	requireReason(t, err, AllocationUnavailable)
}

func TestSystemdZapretPacketMarkMaskExcludesSerial(t *testing.T) {
	request, facts := zapretFixture()
	facts.Capability.Backend = ZapretSystemdBackend
	facts.Zapret.InstalledEgress = &ZapretEgress{
		NamespaceCookie: 11, IfIndex: 2, IPv4Gateway: netip.MustParseAddr("192.0.2.1"), RouteRevision: hash("egress"),
	}
	facts.Zapret.SystemdSerial = ZapretMaxSerial
	plan, err := CompileZapret(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.PacketMarkMask(); got != 0x0f000000 || got&ZapretMaxSerial != 0 {
		t.Fatalf("protected flags must not catch an unrelated mark in the serial field: %#x", got)
	}
	if plan.SocketMark() != ZapretSocketMarkBit|ZapretMaxSerial {
		t.Fatal("the exact socket mark lost its generation serial")
	}
}

func TestZapretDriftChecksAllAllocationsBeforeAllowingRepair(t *testing.T) {
	request, facts := zapretFixture()
	zapret, err := CompileZapret(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	plan := zapret.Plan()
	var actual []Allocation
	for _, resource := range plan.Resources() {
		actual = append(actual, Allocation{Ownership: resource, Proven: true})
	}
	if drift := Compare(plan, plan.Revision(), true, actual); len(drift) != 0 {
		t.Fatalf("complete allocations reported drift: %#v", drift)
	}
	if drift := Compare(plan, plan.Revision(), true, actual[:1]); len(drift) != 5 {
		t.Fatalf("a matching table hid queue or mark loss: %#v", drift)
	}
	queue := actual[1]
	queue.Kind = "foreign-queue"
	drift := Compare(plan, plan.Revision(), true, []Allocation{actual[0], queue})
	if len(drift) != 5 || drift[0].Kind != Unknown || drift[1].Kind != Missing || drift[4].Kind != Missing {
		t.Fatalf("foreign queue was not distinguished from missing marks: %#v", drift)
	}
	for _, entry := range drift {
		if entry.Repairable {
			t.Fatalf("ambiguous allocation still permits partial repair: %#v", drift)
		}
	}
	if drift := Compare(plan, plan.Revision(), true, append(actual, actual[1])); len(drift) != 1 || drift[0].Kind != Unknown || drift[0].Repairable {
		t.Fatalf("duplicate queue observation admitted: %#v", drift)
	}
	if drift := Compare(plan, hash("previous generation"), true, actual); len(drift) != 1 || drift[0].Kind != Stale || drift[0].Repairable {
		t.Fatalf("a late sample can influence a newer generation: %#v", drift)
	}
}
