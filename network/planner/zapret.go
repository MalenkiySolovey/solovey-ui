package planner

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

// ZapretNamespaceBackend is the qualification-only adapter. No production
// platform advertises this capability or executes the resulting plan.
const ZapretNamespaceBackend = "isolated-linux-tc-nft-nfqueue/v2"
const ZapretProbeTable = "solovey_zapret_probe"
const ZapretProbeGuard = "tc:lo:clsact:egress"

// ZapretInstalledBackend is a compile target, not an advertised capability.
// Only an adapter with a live, independent egress guard can admit the plan.
const ZapretInstalledBackend = "native-linux-bpf-nft-nfqueue/v2"
const ZapretInstalledTable = "solovey_zapret"

// ZapretSystemdBackend reserves a mark domain under the trusted single-owner
// Linux profile. A serial in that domain must not be reused within one boot.
const ZapretSystemdBackend = "native-linux-systemd-nft-nfqueue/v1"
const ZapretDenyTable = "solovey_zapret_deny"

const (
	ZapretSocketMarkBit   uint32 = 1 << 24
	zapretSystemdLoopMark uint32 = 1 << 25
	zapretSystemdPassMark uint32 = 1 << 26
	ZapretSystemdFinal    uint32 = 1 << 27
	ZapretMaxSerial       uint32 = (1 << 24) - 1
	ZapretDenyMask               = ZapretSocketMarkBit | zapretSystemdLoopMark
	ZapretReservedMask           = ZapretDenyMask | zapretSystemdPassMark | ZapretSystemdFinal | ZapretMaxSerial
)

// ZapretFacts are supplied under the same operation fence as Facts. An owner
// must account for kernel and core mark/queue users, including masked rules and
// dynamic policies, in the namespace where the socket will be created.
type ZapretFacts struct {
	OutboundRef, OutboundRevision, Provider   string
	DirectSocket, TUNAutoRedirect, SocketMark bool
	NFQueue, ConntrackAccounting              bool
	EgressGuard                               bool
	ReservedMarkBits, ConfiguredSocketMark    uint32
	OccupiedQueues                            []uint16
	InstalledEgress                           *ZapretEgress `json:",omitempty"`
	SystemdSerial                             uint32        `json:",omitempty"`
}

type ZapretEgress struct {
	NamespaceCookie uint64
	IfIndex         int32
	// A zero address means that family has no admitted gateway in table main.
	IPv4Gateway, IPv6Gateway netip.Addr
	// RoutingMarkBits includes kernel RPDB selectors, not firewall writers.
	RoutingMarkBits uint32
	RouteRevision   string
}

func (e ZapretEgress) valid() bool {
	return e.NamespaceCookie != 0 && e.IfIndex > 0 && digest(e.RouteRevision) &&
		(e.IPv4Gateway.IsValid() || e.IPv6Gateway.IsValid()) &&
		(!e.IPv4Gateway.IsValid() || e.IPv4Gateway.Is4() && zapretGateway(e.IPv4Gateway)) &&
		(!e.IPv6Gateway.IsValid() || e.IPv6Gateway.Is6() && !e.IPv6Gateway.Is4In6() && zapretGateway(e.IPv6Gateway))
}

func zapretGateway(addr netip.Addr) bool {
	return addr.Zone() == "" && (addr.IsGlobalUnicast() || addr.IsLinkLocalUnicast())
}

type ZapretPlan struct {
	plan     Plan
	intent   intent.ZapretPacketPathIntent
	queue    uint16
	mark     uint32
	loopMark uint32
	passMark uint32
	finalBit uint32
}

func (p ZapretPlan) Plan() Plan         { return p.plan }
func (p ZapretPlan) Queue() uint16      { return p.queue }
func (p ZapretPlan) SocketMark() uint32 { return p.mark }
func (p ZapretPlan) LoopMark() uint32   { return p.loopMark }

// QueueMark includes the admission bit issued by the nft queue rule, never by
// the dialer. The egress guard drops the unapproved socket mark.
func (p ZapretPlan) QueueMark() uint32 { return p.mark | p.passMark }

// RawOutputMark distinguishes generator packets which passed LOCAL_OUT from
// the two input marks nfqws2 uses for its raw sends.
func (p ZapretPlan) RawOutputMark() uint32 { return p.loopMark | p.passMark }

// FinalRouteBit is the installed firewall's final route proof bit. Only
// POSTROUTING applies it, for both queued originals and generator output.
// It is never inherited by NFQUEUE or the engine's sendto.
func (p ZapretPlan) FinalRouteBit() uint32 { return p.finalBit }

// PacketMarkMask identifies our packet domain, not an exact generation.
// The systemd serial is meaningful only on marks with a Zapret flag.
func (p ZapretPlan) PacketMarkMask() uint32 {
	if p.plan.facts.Capability.Backend == ZapretSystemdBackend {
		return ZapretReservedMask &^ ZapretMaxSerial
	}
	return p.QueueMark() | p.LoopMark() | p.FinalRouteBit()
}

func (p ZapretPlan) PacketScope() intent.ZapretPacketPathIntent {
	return intent.CanonicalZapretPacketPathIntent(p.intent)
}

// ZapretSocketScope projects allocation data for the installed broker. It is
// not an activation receipt: validation or compilation alone grants no rights.
type ZapretSocketScope struct{ plan ZapretPlan }

func (p ZapretPlan) SocketScope() (ZapretSocketScope, error) {
	backend := p.plan.facts.Capability.Backend
	if backend != ZapretInstalledBackend && backend != ZapretSystemdBackend || p.plan.facts.Zapret == nil ||
		p.plan.facts.Zapret.InstalledEgress == nil || p.mark == 0 {
		return ZapretSocketScope{}, conflict(CapabilityUnavailable, p.intent.ResourceRef)
	}
	return ZapretSocketScope{plan: p}, nil
}

func (s ZapretSocketScope) Valid() bool {
	_, err := s.plan.SocketScope()
	return err == nil
}

func (s ZapretSocketScope) PlanRevision() string { return s.plan.plan.Revision() }
func (s ZapretSocketScope) Mark() uint32         { return s.plan.SocketMark() }
func (s ZapretSocketScope) OutboundRef() string  { return s.plan.intent.OutboundRef }
func (s ZapretSocketScope) StrategyRevision() string {
	return s.plan.intent.StrategyRevision
}
func (s ZapretSocketScope) Egress() ZapretEgress {
	if !s.Valid() {
		return ZapretEgress{}
	}
	return *s.plan.plan.facts.Zapret.InstalledEgress
}
func (s ZapretSocketScope) AllowsProtocol(p intent.PacketProtocol) bool {
	return slices.Contains(s.plan.intent.Protocols, p)
}

func CompileZapret(request intent.ZapretPacketPathIntent, facts Facts) (ZapretPlan, error) {
	if request.Validate() != nil {
		return ZapretPlan{}, conflict(InvalidIntent, "zapret")
	}
	ref := request.ResourceRef
	z := facts.Zapret
	if z == nil || len(z.OccupiedQueues) > 8192 || facts.HostRef != request.HostRef {
		return ZapretPlan{}, conflict(ObservationUnknown, ref)
	}
	if z.OutboundRef != request.OutboundRef || z.OutboundRevision != request.InputRevision {
		return ZapretPlan{}, conflict(StalePlan, ref)
	}
	if request.NeedsReturn || !z.DirectSocket || z.TUNAutoRedirect || !z.SocketMark ||
		!z.NFQueue || !z.ConntrackAccounting || !z.EgressGuard || z.Provider != "sing-box" {
		return ZapretPlan{}, conflict(CapabilityUnavailable, ref)
	}
	table, guard := "inet:"+ZapretProbeTable, ZapretProbeGuard
	switch facts.Capability.Backend {
	case ZapretNamespaceBackend:
		if z.InstalledEgress != nil {
			return ZapretPlan{}, conflict(CapabilityUnavailable, ref)
		}
	case ZapretInstalledBackend:
		if z.InstalledEgress == nil || !z.InstalledEgress.valid() {
			return ZapretPlan{}, conflict(CapabilityUnavailable, ref)
		}
		table = "inet:" + ZapretInstalledTable
		guard = fmt.Sprintf("nf-postrouting:netns:%d", z.InstalledEgress.NamespaceCookie)
	case ZapretSystemdBackend:
		if z.InstalledEgress == nil || !z.InstalledEgress.valid() || !z.InstalledEgress.IPv4Gateway.IsValid() || z.InstalledEgress.IPv6Gateway.IsValid() ||
			z.SystemdSerial == 0 || z.SystemdSerial > ZapretMaxSerial || len(request.TCPPorts) == 0 || len(request.UDPPorts) != 0 ||
			(z.InstalledEgress.RoutingMarkBits|z.ReservedMarkBits)&ZapretReservedMask != 0 {
			return ZapretPlan{}, conflict(CapabilityUnavailable, ref)
		}
		table, guard = "inet:"+ZapretInstalledTable, "inet:"+ZapretDenyTable
	default:
		return ZapretPlan{}, conflict(CapabilityUnavailable, ref)
	}
	if z.ConfiguredSocketMark != 0 {
		return ZapretPlan{}, conflict(ResourceConflict, ref)
	}
	request = intent.CanonicalZapretPacketPathIntent(request)
	value := Intent{HostRef: request.HostRef, Owner: request.Owner, ResourceRef: ref,
		InputRevision: request.InputRevision, DesiredRevision: request.DesiredRevision,
		ConfigurationRevision: revision(request), Purpose: request.Purpose}
	if err := validatePlanInputs(value, facts); err != nil {
		return ZapretPlan{}, err
	}
	if len(facts.Allocations) != 0 {
		// Initial apply never adopts resources based on a familiar name or
		// mark. Recovery must compare the live object with a committed receipt.
		for _, allocation := range facts.Allocations {
			if allocation.Kind == "nfqueue" || allocation.Kind == "socket_mark" || allocation.Kind == "loop_mark" ||
				allocation.Kind == "packet_pass_mark" || allocation.Allocation == table || allocation.Allocation == guard {
				return ZapretPlan{}, conflict(ResourceConflict, ref)
			}
		}
	}
	occupied := slices.Clone(z.OccupiedQueues)
	slices.Sort(occupied)
	occupied = slices.Compact(occupied)
	zapretFacts := *z
	zapretFacts.OccupiedQueues = occupied
	facts.Zapret = &zapretFacts
	plan := newPlan(value, canonicalFacts(facts))
	seed, _ := strconv.ParseUint(plan.revision[:4], 16, 16)
	var queue uint16
	for offset := uint64(0); offset <= uint64(len(occupied)); offset++ {
		candidate := uint16(1 + (seed+offset)%65535)
		if _, found := slices.BinarySearch(occupied, candidate); !found {
			queue = candidate
			break
		}
	}
	var mark, loopMark, passMark, finalBit uint32
	reserved := z.ReservedMarkBits
	if z.InstalledEgress != nil {
		reserved |= z.InstalledEgress.RoutingMarkBits
	}
	// Keep the marks in the nonnegative range expected by both
	// nfqws2 CLI parsing and the provider's Linux socket control.
	for offset := range 31 {
		bit := uint32(1) << ((int(seed) + offset) % 31)
		if reserved&bit != 0 {
			continue
		}
		if mark == 0 {
			mark = bit
		} else if loopMark == 0 {
			loopMark = bit
		} else if passMark == 0 {
			passMark = bit
			if z.InstalledEgress == nil {
				break
			}
		} else {
			finalBit = bit
			break
		}
	}
	if facts.Capability.Backend == ZapretSystemdBackend {
		mark, loopMark, passMark, finalBit = ZapretSocketMarkBit|z.SystemdSerial, zapretSystemdLoopMark, zapretSystemdPassMark, ZapretSystemdFinal
	}
	if queue == 0 || passMark == 0 || z.InstalledEgress != nil && finalBit == 0 {
		return ZapretPlan{}, conflict(AllocationUnavailable, ref)
	}
	type claim struct{ kind, allocation string }
	claims := []claim{
		{"filter_table", table},
		{"nfqueue", fmt.Sprintf("nfqueue:%d", queue)},
		{"socket_mark", fmt.Sprintf("mark:0x%x", mark)},
		{"loop_mark", fmt.Sprintf("mark:0x%x", loopMark)},
		{"packet_pass_mark", fmt.Sprintf("mark:0x%x", passMark)},
		{"egress_guard", guard},
	}
	if finalBit != 0 {
		claims = append(claims, claim{"packet_pass_mark", fmt.Sprintf("mark:0x%x", finalBit)})
	}
	for _, claim := range claims {
		plan.resources = append(plan.resources, Ownership{HostRef: request.HostRef, Owner: request.Owner,
			ResourceRef: ref, PlanRevision: plan.revision, Kind: claim.kind, Purpose: request.Purpose, Allocation: claim.allocation})
	}
	result := ZapretPlan{plan: plan, intent: request, queue: queue, mark: mark, loopMark: loopMark, passMark: passMark, finalBit: finalBit}
	result.plan.zapret = &request
	return result, nil
}
