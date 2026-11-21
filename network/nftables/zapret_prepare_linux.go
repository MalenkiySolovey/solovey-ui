//go:build linux && !minimal

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
)

// ZapretNativePath is a broker-local generation. Its initial kernel lease
// denies all protected packets. Construction does not authorize advertising
// a listener or reusing a durable receipt from a previous process.
type ZapretNativePath struct {
	mu         sync.Mutex
	plan       planner.ZapretPlan
	engine     processevidence.Fact
	queue      ZapretQueueOwner
	guard      *ZapretNativeGuard
	table      *ZapretOwnedTable
	generation string
	enrolled   bool
	closed     bool
}

// PrepareZapretNativePath stages the exact Planner allocation. The caller is
// the privileged owner, must supply real observations and the trusted launch
// identity, and must durably commit before renewing or publishing a grant.
func PrepareZapretNativePath(ctx context.Context, plan planner.ZapretPlan, before planner.Facts, pinDir string, engine processevidence.Fact) (_ *ZapretNativePath, failure error) {
	if err := planner.CheckCurrent(plan.Plan(), before); err != nil {
		return nil, err
	}
	queue, err := ObserveZapretQueueOwner(plan, engine)
	if err != nil {
		return nil, err
	}
	var generation [32]byte
	_, _ = rand.Read(generation[:])
	p := &ZapretNativePath{plan: plan, engine: engine, queue: queue, generation: hex.EncodeToString(generation[:])}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, p.Close())
		}
	}()
	p.guard, err = CreateZapretNativeGuard(plan, pinDir)
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

// Renew is called only after the owning operation commits and whenever its
// fresh route/allocation observation confirms the before-image. The trusted
// observer may exclude only this exact path's verified live allocations.
func (p *ZapretNativePath) Renew(ctx context.Context, current planner.Facts, lifetime time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.verify(ctx, current); err != nil {
		return errors.Join(err, p.closeLocked())
	}
	if err := p.guard.Renew(lifetime); err != nil {
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
	if p.closed || !scope.Valid() || scope.PlanRevision() != p.plan.Plan().Revision() || generation != p.generation || action == nil || observe == nil {
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
	p.enrolled = true
	return action()
}

func (p *ZapretNativePath) verify(ctx context.Context, current planner.Facts) error {
	if p.closed || p.guard == nil || p.table == nil {
		return errors.New("Zapret native path is unavailable")
	}
	if err := planner.CheckCurrent(p.plan.Plan(), current); err != nil {
		return err
	}
	if err := p.table.Verify(ctx); err != nil {
		return err
	}
	queue, err := ObserveZapretQueueOwner(p.plan, p.engine)
	if err != nil || queue != p.queue {
		return errors.Join(errors.New("Zapret queue ownership drift"), err)
	}
	return ctx.Err()
}

func (p *ZapretNativePath) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeLocked()
}

func (p *ZapretNativePath) closeLocked() error {
	if p.closed {
		return nil
	}
	p.closed = true
	var revokeErr, tableErr, guardErr error
	if p.guard != nil {
		revokeErr = p.guard.Revoke()
	}
	if p.table != nil {
		tableErr = p.table.Close()
	}
	if p.guard != nil {
		if p.enrolled {
			guardErr = p.guard.Close() // Pinned deny survives old issued FDs.
		} else {
			guardErr = p.guard.AbortUnadmitted()
		}
	}
	return errors.Join(revokeErr, tableErr, guardErr)
}
