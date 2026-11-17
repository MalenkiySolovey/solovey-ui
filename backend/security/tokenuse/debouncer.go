// Package tokenuse owns batched API token usage and database generation fencing.
package tokenuse

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
)

const (
	DefaultInterval        = time.Minute
	BatchSize              = 100
	tokenUseResetQuiet     = 5 * time.Second
	tokenUseFailureBackoff = 5 * time.Minute
)

type Update struct {
	IP string
	At int64
}

type Debouncer struct {
	Warning        func(...any)
	mu             sync.Mutex
	writeMu        sync.Mutex
	pending        map[uint]Update
	timer          *time.Timer
	epoch          uint64
	interval       time.Duration
	failureBackoff time.Duration
	circuitUntil   time.Time
	flush          func(map[uint]Update) error
	retired        bool
}

func New(interval time.Duration, flush func(map[uint]Update) error) *Debouncer {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Debouncer{
		pending:        make(map[uint]Update),
		interval:       interval,
		failureBackoff: tokenUseFailureBackoff,
		flush:          flush,
	}
}

func Stop(ctx context.Context, debouncer *Debouncer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	finishReset, ok := beginTokenUseStop()
	if !ok {
		return ctx.Err()
	}
	defer finishReset()
	if debouncer == nil {
		return ctx.Err()
	}
	return debouncer.flushNow(ctx, true, true)
}

func (d *Debouncer) Record(id uint, ip string, ts int64) {
	if id == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.retired {
		return
	}
	d.pending[id] = Update{IP: ip, At: ts}
	d.scheduleLocked()
}

func (d *Debouncer) scheduleLocked() {
	if d.retired || d.timer != nil {
		return
	}
	now := time.Now()
	epoch := d.epoch
	delay := d.scheduleDelayLocked(now)
	d.timer = time.AfterFunc(delay, func() {
		d.flushTimer(epoch)
	})
}

func (d *Debouncer) flushTimer(epoch uint64) {
	releaseFlush, ok := beginTokenUseFlush()
	if !ok {
		d.mu.Lock()
		if epoch == d.epoch {
			d.timer = nil
			if len(d.pending) > 0 {
				d.scheduleLocked()
			}
		}
		d.mu.Unlock()
		return
	}
	defer releaseFlush()
	if d.timerCircuitOpen(epoch, time.Now()) {
		return
	}
	updates := d.takePending(epoch)
	if len(updates) == 0 {
		return
	}
	if err := d.write(updates); err != nil {
		if d.Warning != nil {
			d.Warning("token use flush failed:", err)
		}
		d.requeueAfterWriteError(updates, time.Now())
		return
	}
	d.closeFailureCircuit()
	d.mu.Lock()
	if len(d.pending) > 0 {
		d.scheduleLocked()
	}
	d.mu.Unlock()
}

func (d *Debouncer) Flush(ctx context.Context) error {
	return d.flushNow(ctx, false, true)
}

func (d *Debouncer) flushNow(ctx context.Context, bypassGate bool, requeueOnError bool) error {
	var releaseFlush func()
	if !bypassGate {
		var ok bool
		releaseFlush, ok = beginTokenUseFlush()
		if !ok {
			return nil
		}
		defer releaseFlush()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	if d.retired {
		d.mu.Unlock()
		return nil
	}
	d.epoch++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	updates := d.pending
	d.pending = make(map[uint]Update)
	d.mu.Unlock()
	if len(updates) == 0 {
		return nil
	}

	err := d.write(updates)
	if err == nil {
		d.closeFailureCircuit()
	} else if requeueOnError {
		d.requeueAfterWriteError(updates, time.Now())
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func (d *Debouncer) takePending(epoch uint64) map[uint]Update {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.retired || epoch != d.epoch {
		return nil
	}
	updates := d.pending
	d.pending = make(map[uint]Update)
	d.timer = nil
	return updates
}

func (d *Debouncer) Retire() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.retired = true
	d.epoch++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.pending = make(map[uint]Update)
}

func (d *Debouncer) write(updates map[uint]Update) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.flush == nil || len(updates) == 0 {
		return nil
	}
	return d.flush(updates)
}

func (d *Debouncer) scheduleDelayLocked(now time.Time) time.Duration {
	if d.circuitUntil.After(now) {
		return d.circuitUntil.Sub(now)
	}
	return d.interval
}

func (d *Debouncer) timerCircuitOpen(epoch uint64, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if epoch != d.epoch {
		return true
	}
	if !d.circuitUntil.After(now) {
		return false
	}
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if len(d.pending) > 0 {
		d.scheduleLocked()
	}
	return true
}

func (d *Debouncer) requeueAfterWriteError(updates map[uint]Update, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mergeTokenUseUpdatesLocked(updates)
	d.circuitUntil = now.Add(d.failureBackoff)
	d.epoch++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if len(d.pending) > 0 {
		d.scheduleLocked()
	}
}

func (d *Debouncer) closeFailureCircuit() {
	d.mu.Lock()
	hadCircuit := !d.circuitUntil.IsZero()
	d.circuitUntil = time.Time{}
	if hadCircuit && len(d.pending) > 0 {
		if d.timer != nil {
			d.timer.Stop()
			d.timer = nil
			d.epoch++
		}
		d.scheduleLocked()
	}
	d.mu.Unlock()
}

func (d *Debouncer) mergeTokenUseUpdatesLocked(updates map[uint]Update) {
	for id, update := range updates {
		if current, ok := d.pending[id]; !ok || update.At > current.At {
			d.pending[id] = update
		}
	}
}

var tokenUseFlushGate = struct {
	sync.RWMutex
	suspended bool
	timer     *time.Timer
}{}

func beginTokenUseFlush() (func(), bool) {
	tokenUseFlushGate.RLock()
	if tokenUseFlushGate.suspended {
		tokenUseFlushGate.RUnlock()
		return nil, false
	}
	return tokenUseFlushGate.RUnlock, true
}

func BeginReset() func() {
	tokenUseFlushGate.Lock()
	tokenUseFlushGate.suspended = true
	if tokenUseFlushGate.timer != nil {
		tokenUseFlushGate.timer.Stop()
		tokenUseFlushGate.timer = nil
	}
	return func() {
		tokenUseFlushGate.timer = time.AfterFunc(tokenUseResetQuiet, Resume)
		tokenUseFlushGate.Unlock()
	}
}

func beginTokenUseStop() (func(), bool) {
	tokenUseFlushGate.Lock()
	if tokenUseFlushGate.suspended {
		if tokenUseFlushGate.timer != nil {
			tokenUseFlushGate.timer.Stop()
		}
		tokenUseFlushGate.timer = time.AfterFunc(tokenUseResetQuiet, Resume)
		tokenUseFlushGate.Unlock()
		return nil, false
	}
	tokenUseFlushGate.suspended = true
	if tokenUseFlushGate.timer != nil {
		tokenUseFlushGate.timer.Stop()
		tokenUseFlushGate.timer = nil
	}
	return func() {
		tokenUseFlushGate.timer = time.AfterFunc(tokenUseResetQuiet, Resume)
		tokenUseFlushGate.Unlock()
	}, true
}

func Resume() {
	tokenUseFlushGate.Lock()
	tokenUseFlushGate.suspended = false
	if tokenUseFlushGate.timer != nil {
		tokenUseFlushGate.timer.Stop()
		tokenUseFlushGate.timer = nil
	}
	tokenUseFlushGate.Unlock()
}

func FlushUpdates(updates map[uint]Update) error {
	_, release, err := dbsqlite.AcquireOperation(context.Background())
	if err != nil {
		return err
	}
	defer release()
	db := dbsqlite.DB()
	if db == nil || len(updates) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(updates))
	for id := range updates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for start := 0; start < len(ids); start += BatchSize {
		end := start + BatchSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := flushBatch(ids[start:end], updates); err != nil {
			return err
		}
	}
	return nil
}

func flushBatch(ids []uint, updates map[uint]Update) error {
	if len(ids) == 0 {
		return nil
	}
	var query strings.Builder
	args := make([]any, 0, len(ids)*5)
	query.WriteString("UPDATE tokens SET last_used_at = CASE id")
	for _, id := range ids {
		update := updates[id]
		query.WriteString(" WHEN ? THEN ?")
		args = append(args, id, update.At)
	}
	query.WriteString(" END, last_used_ip = CASE id")
	for _, id := range ids {
		update := updates[id]
		query.WriteString(" WHEN ? THEN ?")
		args = append(args, id, update.IP)
	}
	query.WriteString(" END WHERE id IN (")
	for i, id := range ids {
		if i > 0 {
			query.WriteByte(',')
		}
		query.WriteByte('?')
		args = append(args, id)
	}
	query.WriteByte(')')
	if err := dbsqlite.DB().Exec(query.String(), args...).Error; err != nil {
		return fmt.Errorf("flush token use batch: %w", err)
	}
	return nil
}

// FlushForReset drains the retiring database generation while the reset gate is held.
func (d *Debouncer) FlushForReset(ctx context.Context) error { return d.flushNow(ctx, true, false) }
