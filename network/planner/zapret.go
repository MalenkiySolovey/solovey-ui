package planner

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

// ZapretNamespaceBackend is the qualification-only adapter. No production
// platform advertises this capability or executes the resulting plan.
const ZapretNamespaceBackend = "isolated-linux-tc-nft-nfqueue/v2"
const ZapretProbeTable = "solovey_zapret_probe"
const ZapretProbeGuard = "tc:lo:clsact:egress"

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
}

type ZapretPlan struct {
	plan     Plan
	intent   intent.ZapretPacketPathIntent
	queue    uint16
	mark     uint32
	loopMark uint32
	passMark uint32
}

func (p ZapretPlan) Plan() Plan         { return p.plan }
func (p ZapretPlan) Queue() uint16      { return p.queue }
func (p ZapretPlan) SocketMark() uint32 { return p.mark }
func (p ZapretPlan) LoopMark() uint32   { return p.loopMark }

// QueueMark includes the admission bit issued by the nft queue rule, never by
// the dialer. The egress guard drops the unapproved socket mark.
func (p ZapretPlan) QueueMark() uint32 { return p.mark | p.passMark }
func (p ZapretPlan) PacketScope() intent.ZapretPacketPathIntent {
	return intent.CanonicalZapretPacketPathIntent(p.intent)
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
	if facts.Capability.Backend != ZapretNamespaceBackend || request.NeedsReturn || !z.DirectSocket || z.TUNAutoRedirect || !z.SocketMark ||
		!z.NFQueue || !z.ConntrackAccounting || !z.EgressGuard || z.Provider != "sing-box" {
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
		// The qualification adapter admits only a fresh namespace. It never
		// adopts, replaces or cleans up a pre-existing resource.
		for _, allocation := range facts.Allocations {
			if allocation.Kind == "nfqueue" || allocation.Kind == "socket_mark" || allocation.Kind == "loop_mark" ||
				allocation.Kind == "packet_pass_mark" || allocation.Allocation == "inet:"+ZapretProbeTable ||
				allocation.Allocation == ZapretProbeGuard {
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
	var mark, loopMark, passMark uint32
	// Keep the test's marks in the nonnegative range expected by both
	// nfqws2 CLI parsing and the provider's Linux socket control.
	for offset := range 31 {
		bit := uint32(1) << ((int(seed) + offset) % 31)
		if z.ReservedMarkBits&bit != 0 {
			continue
		}
		if mark == 0 {
			mark = bit
		} else if loopMark == 0 {
			loopMark = bit
		} else {
			passMark = bit
			break
		}
	}
	if queue == 0 || passMark == 0 {
		return ZapretPlan{}, conflict(AllocationUnavailable, ref)
	}
	for _, claim := range []struct{ kind, allocation string }{
		{"filter_table", "inet:" + ZapretProbeTable},
		{"nfqueue", fmt.Sprintf("nfqueue:%d", queue)},
		{"socket_mark", fmt.Sprintf("mark:0x%x", mark)},
		{"loop_mark", fmt.Sprintf("mark:0x%x", loopMark)},
		{"packet_pass_mark", fmt.Sprintf("mark:0x%x", passMark)},
		{"egress_guard", ZapretProbeGuard},
	} {
		plan.resources = append(plan.resources, Ownership{HostRef: request.HostRef, Owner: request.Owner,
			ResourceRef: ref, PlanRevision: plan.revision, Kind: claim.kind, Purpose: request.Purpose, Allocation: claim.allocation})
	}
	result := ZapretPlan{plan: plan, intent: request, queue: queue, mark: mark, loopMark: loopMark, passMark: passMark}
	result.plan.zapret = &request
	return result, nil
}
