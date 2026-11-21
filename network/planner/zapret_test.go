package planner

import (
	"errors"
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
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true},
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
	if err != nil || first.Queue() != second.Queue() || first.SocketMark() != second.SocketMark() || first.LoopMark() != second.LoopMark() {
		t.Fatalf("allocation is not deterministic: first=%#v second=%#v err=%v", first, second, err)
	}
	if first.Queue() == 0 || first.SocketMark() == 0 || first.LoopMark() == 0 || first.SocketMark() == first.LoopMark() {
		t.Fatalf("invalid allocation: queue=%d mark=%x loop=%x", first.Queue(), first.SocketMark(), first.LoopMark())
	}
	if len(first.Plan().Resources()) != 4 {
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

func TestCompileZapretFailsClosedForUnsupportedPathAndCollision(t *testing.T) {
	for name, edit := range map[string]func(*intent.ZapretPacketPathIntent, *Facts){
		"tun auto redirect": func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.TUNAutoRedirect = true },
		"no accounting":     func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.ConntrackAccounting = false },
		"mark collision":    func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.ConfiguredSocketMark = 1 },
		"no free marks":     func(_ *intent.ZapretPacketPathIntent, f *Facts) { f.Zapret.ReservedMarkBits = ^uint32(1) },
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
