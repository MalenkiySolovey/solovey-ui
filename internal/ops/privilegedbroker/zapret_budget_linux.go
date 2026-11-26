//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"golang.org/x/sys/unix"
)

var errZapretDescriptorBudget = errors.New("installed Zapret broker descriptor budget exceeded")
var errZapretMarkBudget = errors.New("installed Zapret socket mark budget exceeded")

// ErrZapretEngineBudget classifies an argument size error from the compiler.
var ErrZapretEngineBudget = errors.New("installed Zapret engine capacity exceeded")

const (
	// Listener plus one accept before the worker gate; each admitted worker
	// can hold the peer stream, pidfd and up to two received descriptors.
	zapretBindingFDs = 2 + zapretSocketWorkers*(1+1+2)
	// Two table owners, FD-store transfer, retained peer and child pidfds,
	// trace pipe, and transient netlink/journal/proc operations.
	zapretBatchFDs = 2 + 2 + 2 + 2 + 4
	// A prepared device, handoff listener and stream, and peer/recheck pidfds.
	zapretTUNFDs = 1 + 1 + 1 + 2
)

func checkZapretDescriptorBudget(bindings int, tun bool) error {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		return fmt.Errorf("%w: could not read RLIMIT_NOFILE: %w", errZapretDescriptorBudget, err)
	}
	open, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return fmt.Errorf("%w: could not account for open broker descriptors: %w", errZapretDescriptorBudget, err)
	}
	capacity := zapretBindingDescriptorCapacity(limit.Cur, uint64(len(open)), tun)
	if uint64(bindings) > capacity {
		return fmt.Errorf("%w: %d bindings requested, %d fit the available broker descriptors; disable a binding or review the broker's LimitNOFILE", errZapretDescriptorBudget, bindings, capacity)
	}
	return nil
}

func zapretBindingDescriptorCapacity(limit, open uint64, tun bool) uint64 {
	overhead := uint64(zapretBatchFDs)
	if tun {
		overhead += zapretTUNFDs
	}
	if open >= limit || limit-open < overhead {
		return 0
	}
	return (limit - open - overhead) / zapretBindingFDs
}

func zapretCapacityFailure(err error) bool {
	return errors.Is(err, errZapretDescriptorBudget) || errors.Is(err, errZapretMarkBudget) ||
		errors.Is(err, ErrZapretEngineBudget) || errors.Is(err, nftables.ErrZapretAtomicBudget) ||
		errors.Is(err, errZapretMemoryBudget) || errors.Is(err, errZapretMemoryEvidence)
}

// Called under the broker mutation fence; an old peer cannot borrow another
// generation's process or network observation.
func (z *installedZapret) activeForPeer(peer PeerIdentity) *installedZapretPath {
	path := z.active
	if path == nil || path.peer != peer.Revision || path.uid != peer.UID || path.ctx.Err() != nil {
		return nil
	}
	return path
}

func (z *installedZapret) checkBatchBudget(ctx context.Context, input zapretactivation.Validation, peer PeerIdentity, profile installedZapretProfile, selected InstalledZapretStrategies) error {
	var verifyActive func(context.Context) error
	var activePID int
	if path := z.activeForPeer(peer); path != nil {
		verifyActive = path.verify
		activePID = path.enginePID
	}
	replies := selected.needsReturn()
	egress, occupied, err := observeInstalledZapretNetwork(ctx, profile.Interface, profile.IPv6, input.TUN != nil, replies, z.deny.port != 0, verifyActive)
	if err != nil {
		return err
	}
	if err := profile.verifyDialBindings(input.Bindings, egress.IfIndex); err != nil {
		return err
	}
	first, err := z.journal.previewZapretMarkBatch(peer, len(input.Bindings))
	if err != nil {
		return err
	}
	facts := make([]planner.Facts, len(input.Bindings))
	for i, request := range input.Bindings {
		facts[i] = installedZapretFacts(request, &egress, occupied, first+uint32(i))
		facts[i].Zapret.SystemdFamilies = &selected.Families[i]
		facts[i].Zapret.SystemdReplyGuard = replies
	}
	plans, err := planner.CompileZapretSystemdBatch(selected.Intents, facts)
	if err != nil {
		return err
	}
	// Queue selection may change after retirement. Charge the longest native
	// uint16 spelling; the binary rules use fixed-width queue and mark fields.
	args, _, err := compileInstalledZapret(plans, selected, math.MaxUint16, profile.ObserveNativeProfiles)
	if err != nil {
		return err
	}
	if err := checkZapretMemoryBudget(ctx, args, activePID); err != nil {
		return err
	}
	var tun *planner.ZapretTUNForward
	if input.TUN != nil {
		if egress.RoutingMarkBits&planner.ZapretTUNForwardBit != 0 {
			return errors.New("a foreign routing rule claims the Zapret TUN forward bit")
		}
		// verifyTUN already checked the scope. Both interface indices have a
		// fixed on-wire width; preview does not create a TUN device.
		tun = &planner.ZapretTUNForward{Scope: profile.TUN.Scope, IngressIfIndex: 1, DeviceIfIndex: 2}
	}
	return nftables.CheckZapretOwnedBatchBudget(ctx, plans, tun)
}

func compileInstalledZapret(plans []planner.ZapretPlan, selected InstalledZapretStrategies, queue uint16, observe bool) ([]string, ZapretNativeTrace, error) {
	marks := make([]uint32, len(plans))
	replies := false
	for i, plan := range plans {
		marks[i] = plan.QueueMark()
		replies = replies || plan.NeedsReturn()
	}
	return selected.Compile(zapretEngineRoot, queue, plans[0].LoopMark(), marks, observe, replies)
}
