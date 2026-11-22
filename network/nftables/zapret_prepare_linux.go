//go:build linux && !minimal && zapretproof

package nftables

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"golang.org/x/sys/unix"
)

// ZapretNativePath is a broker-local generation. Its initial kernel lease
// denies all protected packets. Construction does not authorize advertising
// a listener or reusing a durable receipt from a previous process.
type ZapretNativePath struct {
	mu                 sync.Mutex
	plan               planner.ZapretPlan
	engine             processevidence.Fact
	sockets            zapretEngineSockets
	guard              *ZapretNativeGuard
	table              *ZapretOwnedTable
	generation         string
	lifetime           context.Context
	cancel             context.CancelFunc
	grantDeadline      uint64
	commitFirstLease   func(context.Context) error
	committed          bool
	mayHaveLeased      bool
	publicationClaimed bool
	closed             bool
	closeErr           error
}

// ZapretPathCheckpoint identifies the owned resources for recovery. It is not
// a socket grant and is never sufficient to reactivate a previous generation.
type ZapretPathCheckpoint struct {
	Generation, PlanRevision        string
	Egress                          planner.ZapretEgress
	SocketMark, QueueMark, LoopMark uint32
	FinalRouteBit, TablePort        uint32
	TableHandle                     uint64
	TableName                       string
	GuardLinks                      [2]uint32
	GuardSlots                      [2]ZapretGuardSlot
	QueueOwner                      ZapretQueueOwner
	RawSocketCookies                [2]uint64
}

// ExcludesPriorRegistration compares two owner-generated checkpoints. A
// fresh create-only table and both fresh BPF attachments at the same slots
// prove the previous registration is absent. Linux rejects an occupied
// BPF hook priority (nf_hook_entries_grow), including FD-only attachments.
func (c ZapretPathCheckpoint) ExcludesPriorRegistration(old ZapretPathCheckpoint) bool {
	return c.Egress.NamespaceCookie != 0 && c.Egress.NamespaceCookie == old.Egress.NamespaceCookie &&
		c.TableName == planner.ZapretInstalledTable && c.TableName == old.TableName &&
		c.GuardSlots[0].Family != 0 && c.GuardSlots[1].Family != 0 && c.GuardSlots == old.GuardSlots &&
		c.GuardLinks[0] != 0 && c.GuardLinks[1] != 0 && old.GuardLinks[0] != 0 && old.GuardLinks[1] != 0 &&
		c.GuardLinks[0] != old.GuardLinks[0] && c.GuardLinks[1] != old.GuardLinks[1]
}

// PrepareZapretNativePath stages the exact Planner allocation. The caller is
// the privileged owner, must supply real observations and the trusted launch
// identity. Grant validity begins at staging and bounds every kernel lease.
// A staged path cannot renew or admit sockets.
func PrepareZapretNativePath(ctx context.Context, plan planner.ZapretPlan, before planner.Facts, pinDir string, engine processevidence.Fact, grantLifetime time.Duration) (_ *ZapretNativePath, failure error) {
	if grantLifetime <= 0 {
		return nil, errors.New("invalid Zapret generation lifetime")
	}
	expires := time.Now().Add(grantLifetime)
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err != nil {
		return nil, err
	}
	// Sample the remaining time after the kernel clock so a scheduling
	// delay can shorten the bound but cannot extend it past expires.
	remaining := time.Until(expires)
	if remaining <= 0 {
		return nil, errors.New("generation for Zapret expired before staging")
	}
	grantDeadline := uint64(now.Nano()) + uint64(remaining)
	if err := planner.CheckCurrent(plan.Plan(), before); err != nil {
		return nil, err
	}
	if err := verifyZapretRoute(ctx, plan); err != nil {
		return nil, err
	}
	sockets, err := observeZapretEngineSockets(plan, engine)
	if err != nil {
		return nil, err
	}
	var generation [32]byte
	_, _ = rand.Read(generation[:])
	lifetime, cancel := context.WithDeadline(context.Background(), expires)
	p := &ZapretNativePath{plan: plan, engine: engine, sockets: sockets, generation: hex.EncodeToString(generation[:]),
		lifetime: lifetime, cancel: cancel, grantDeadline: grantDeadline}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, p.Close())
		}
	}()
	p.guard, err = stageZapretNativeGuard(plan, pinDir, sockets.raw)
	if err != nil {
		return nil, err
	}
	p.table, err = CreateZapretOwnedTable(ctx, plan)
	if err != nil {
		return nil, err
	}
	if err := p.verify(ctx, before); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *ZapretNativePath) Generation() string { return p.generation }

// Lifetime ends at the grant deadline or when the generation is retired.
// Admission listeners stop accepting and drain their pending requests then.
func (p *ZapretNativePath) Lifetime() context.Context { return p.lifetime }

// Commit records the verified generation through the owning broker journal.
// A failed write or changed observation retires the path, even if the write
// left a complete-looking record on disk. The firstLease callback persists the
// irreversible transition before the first kernel lease under this same fence.
func (p *ZapretNativePath) Commit(ctx context.Context, observe func(context.Context) (planner.Facts, error), record, firstLease func(context.Context, ZapretPathCheckpoint) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.committed || observe == nil || record == nil || firstLease == nil {
		return errors.New("path for Zapret cannot be committed")
	}
	verify := func() error {
		facts, err := observe(ctx)
		if err != nil {
			return err
		}
		return p.verify(ctx, facts)
	}
	if err := verify(); err != nil {
		return errors.Join(err, p.closeLocked())
	}
	ids, err := p.guard.verifiedLinks()
	if err != nil {
		return errors.Join(err, p.closeLocked())
	}
	scope, err := p.plan.SocketScope()
	if err != nil {
		return errors.Join(err, p.closeLocked())
	}
	checkpoint := ZapretPathCheckpoint{Generation: p.generation, PlanRevision: scope.PlanRevision(), Egress: scope.Egress(),
		SocketMark: p.plan.SocketMark(), QueueMark: p.plan.QueueMark(), LoopMark: p.plan.LoopMark(), FinalRouteBit: p.plan.FinalRouteBit(),
		TablePort: p.table.port, TableHandle: p.table.handle, TableName: planner.ZapretInstalledTable, GuardLinks: ids, GuardSlots: p.guard.slots,
		QueueOwner: p.sockets.queue, RawSocketCookies: p.sockets.raw}
	if err := record(ctx, checkpoint); err != nil {
		return errors.Join(err, p.closeLocked())
	}
	if err := p.guard.pinLinks(); err != nil {
		return errors.Join(err, p.closeLocked())
	}
	if err := verify(); err != nil {
		return errors.Join(err, p.closeLocked())
	}
	p.commitFirstLease = func(ctx context.Context) error { return firstLease(ctx, checkpoint) }
	p.committed = true
	return nil
}

// AdmissionReady is a publication gate only; every descriptor is fenced
// and rechecked separately before it can receive the allocated mark.
func (p *ZapretNativePath) AdmissionReady(scope planner.ZapretSocketScope, generation string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.admits(scope, generation) && p.guard.leaseActive()
}

// ClaimSocketPublication transfers retirement responsibility to the sole
// listener owner. A losing publisher must not close the live generation.
func (p *ZapretNativePath) ClaimSocketPublication(scope planner.ZapretSocketScope) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.publicationClaimed || !p.admits(scope, p.generation) || !p.guard.leaseActive() {
		return false
	}
	p.publicationClaimed = true
	return true
}

func (p *ZapretNativePath) admits(scope planner.ZapretSocketScope, generation string) bool {
	return !p.closed && p.lifetime.Err() == nil && p.committed && p.mayHaveLeased &&
		scope.Valid() && scope.PlanRevision() == p.plan.Plan().Revision() && generation == p.generation
}

// Renew requires a committed generation whose fresh route/allocation
// observation confirms the before-image. The observer may exclude only this
// exact path's verified live allocations.
func (p *ZapretNativePath) Renew(ctx context.Context, observe func(context.Context) (planner.Facts, error), lifetime time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.committed || observe == nil {
		return errors.New("path for Zapret has no committed generation")
	}
	verify := func() error {
		facts, err := observe(ctx)
		if err != nil {
			return err
		}
		return p.verify(ctx, facts)
	}
	if err := verify(); err != nil {
		return errors.Join(err, p.closeLocked())
	}
	if p.commitFirstLease != nil {
		// The lease also authorizes engine raw output, so the record must
		// precede it even when no Panel socket has been enrolled.
		if err := p.commitFirstLease(ctx); err != nil {
			return errors.Join(err, p.closeLocked())
		}
		p.mayHaveLeased = true
		p.commitFirstLease = nil
		// The fsync can outlast the observations used before it.
		if err := verify(); err != nil {
			return errors.Join(err, p.closeLocked())
		}
	}
	if err := p.guard.renewBefore(lifetime, p.grantDeadline); err != nil {
		return errors.Join(err, p.closeLocked())
	}
	return nil
}

// WithSocket fences an already-attested request against the exact live path.
// A different generation must not disturb the current path. The action
// executes synchronously under the fence, after kernel cookie enrollment.
func (p *ZapretNativePath) WithSocket(ctx context.Context, observe func(context.Context) (planner.Facts, error), scope planner.ZapretSocketScope, generation string, fd int, action func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.admits(scope, generation) || action == nil || observe == nil {
		return errors.New("stale or invalid Zapret path generation")
	}
	current, err := observe(ctx)
	if err != nil {
		return errors.Join(err, p.closeLocked())
	}
	if err := p.verify(ctx, current); err != nil {
		return errors.Join(err, p.closeLocked())
	}
	if err := p.guard.EnrollSocket(fd); err != nil {
		return err
	}
	return action()
}

func (p *ZapretNativePath) verify(ctx context.Context, current planner.Facts) error {
	if p.closed || p.lifetime.Err() != nil || p.guard == nil || p.table == nil {
		return errors.New("native path for Zapret is unavailable")
	}
	if err := planner.CheckCurrent(p.plan.Plan(), current); err != nil {
		return err
	}
	if err := verifyZapretRoute(ctx, p.plan); err != nil {
		return err
	}
	if err := p.table.Verify(ctx); err != nil {
		return err
	}
	if _, err := p.guard.verifiedLinks(); err != nil {
		return err
	}
	sockets, err := observeZapretEngineSockets(p.plan, p.engine)
	if err != nil || sockets != p.sockets {
		return errors.Join(errors.New("queue or generator socket ownership for Zapret changed"), err)
	}
	return ctx.Err()
}

func verifyZapretRoute(ctx context.Context, plan planner.ZapretPlan) error {
	scope, err := plan.SocketScope()
	if err != nil {
		return err
	}
	approved := scope.Egress()
	current, err := planner.ObserveZapretLinuxEgress(ctx, approved.IfIndex, approved.IPv4Gateway.IsValid(), approved.IPv6Gateway.IsValid())
	if err != nil {
		return err
	}
	if current != approved {
		return errors.New("kernel gateway or route revision for Zapret changed")
	}
	return nil
}

func (p *ZapretNativePath) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeLocked()
}

// AbortUnleased retires the exact live generation before any kernel lease.
func (p *ZapretNativePath) AbortUnleased(generation string) error {
	if p == nil {
		return errors.New("path for Zapret is unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.committed || generation != p.generation || p.mayHaveLeased {
		return errors.New("path for Zapret is not a committed, unleased generation")
	}
	return p.closeLocked()
}

func (p *ZapretNativePath) closeLocked() error {
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	if p.cancel != nil {
		defer p.cancel()
	}
	var revokeErr, tableErr, guardErr error
	if p.guard != nil {
		revokeErr = p.guard.Revoke()
	}
	if p.table != nil {
		tableErr = p.table.Close()
	}
	if p.guard != nil {
		if p.mayHaveLeased {
			guardErr = p.guard.Close() // Keep deny hooks for raw and admitted socket output.
		} else {
			guardErr = p.guard.AbortUnleased()
		}
	}
	p.closeErr = errors.Join(revokeErr, tableErr, guardErr)
	return p.closeErr
}
