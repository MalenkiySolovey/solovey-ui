package tokenuse

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTokenUseDebouncerKeepsLatestUseUntilFlush(t *testing.T) {
	written := make(chan map[uint]Update, 1)
	debouncer := New(time.Hour, func(updates map[uint]Update) error {
		copied := make(map[uint]Update, len(updates))
		for id, update := range updates {
			copied[id] = update
		}
		written <- copied
		return nil
	})

	debouncer.Record(7, "198.51.100.1", 100)
	debouncer.Record(7, "198.51.100.2", 200)
	if err := debouncer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	updates := <-written
	if len(updates) != 1 {
		t.Fatalf("expected one debounced token update, got %#v", updates)
	}
	if updates[7].IP != "198.51.100.2" || updates[7].At != 200 {
		t.Fatalf("debouncer did not keep latest token use: %#v", updates[7])
	}
}

func TestTokenUseDebouncerTimerFailureOpensCircuit(t *testing.T) {
	Resume()
	t.Cleanup(Resume)
	errFlush := errors.New("timer flush failed")
	attempts := 0
	var successful map[uint]Update
	debouncer := New(time.Hour, func(updates map[uint]Update) error {
		attempts++
		copied := make(map[uint]Update, len(updates))
		for id, update := range updates {
			copied[id] = update
		}
		if attempts == 1 {
			return errFlush
		}
		successful = copied
		return nil
	})
	debouncer.failureBackoff = time.Hour
	t.Cleanup(func() {
		stopTokenUseDebouncerTimer(debouncer)
	})

	debouncer.Record(7, "198.51.100.1", 100)
	epoch := stopTokenUseDebouncerTimer(debouncer)
	debouncer.flushTimer(epoch)
	if attempts != 1 {
		t.Fatalf("expected first timer write attempt, got %d", attempts)
	}

	debouncer.mu.Lock()
	if len(debouncer.pending) != 1 {
		t.Fatalf("failed update was not requeued: %#v", debouncer.pending)
	}
	if !debouncer.circuitUntil.After(time.Now()) {
		t.Fatalf("circuit was not opened after timer failure: %v", debouncer.circuitUntil)
	}
	debouncer.mu.Unlock()

	debouncer.Record(8, "198.51.100.8", 300)
	epoch = stopTokenUseDebouncerTimer(debouncer)
	debouncer.flushTimer(epoch)
	if attempts != 1 {
		t.Fatalf("open circuit should block immediate retry, got %d attempts", attempts)
	}

	debouncer.mu.Lock()
	debouncer.circuitUntil = time.Now().Add(-time.Second)
	epoch = debouncer.epoch
	if debouncer.timer != nil {
		debouncer.timer.Stop()
		debouncer.timer = nil
	}
	debouncer.mu.Unlock()
	debouncer.flushTimer(epoch)

	if attempts != 2 {
		t.Fatalf("expected retry after circuit cooldown, got %d attempts", attempts)
	}
	if len(successful) != 2 {
		t.Fatalf("expected requeued and newer pending updates, got %#v", successful)
	}
	if successful[7].IP != "198.51.100.1" || successful[7].At != 100 {
		t.Fatalf("failed update was not retried: %#v", successful[7])
	}
	if successful[8].IP != "198.51.100.8" || successful[8].At != 300 {
		t.Fatalf("new pending update was not included in retry: %#v", successful[8])
	}

	debouncer.mu.Lock()
	circuitUntil := debouncer.circuitUntil
	pending := len(debouncer.pending)
	debouncer.mu.Unlock()
	if !circuitUntil.IsZero() {
		t.Fatalf("successful timer flush did not close circuit: %v", circuitUntil)
	}
	if pending != 0 {
		t.Fatalf("pending updates remain after successful retry: %d", pending)
	}
}

func TestTokenUseDebouncerKeepsLatestAfterTimerFailure(t *testing.T) {
	Resume()
	t.Cleanup(Resume)
	errFlush := errors.New("timer flush failed")
	attempts := 0
	var successful map[uint]Update
	var debouncer *Debouncer
	debouncer = New(time.Hour, func(updates map[uint]Update) error {
		attempts++
		copied := make(map[uint]Update, len(updates))
		for id, update := range updates {
			copied[id] = update
		}
		if attempts == 1 {
			debouncer.Record(7, "198.51.100.2", 200)
			return errFlush
		}
		successful = copied
		return nil
	})
	debouncer.failureBackoff = time.Hour
	t.Cleanup(func() {
		stopTokenUseDebouncerTimer(debouncer)
	})

	debouncer.Record(7, "198.51.100.1", 100)
	epoch := stopTokenUseDebouncerTimer(debouncer)
	debouncer.flushTimer(epoch)

	debouncer.mu.Lock()
	if pending := debouncer.pending[7]; pending.IP != "198.51.100.2" || pending.At != 200 {
		t.Fatalf("newer pending update did not win after requeue: %#v", pending)
	}
	debouncer.circuitUntil = time.Now().Add(-time.Second)
	epoch = debouncer.epoch
	if debouncer.timer != nil {
		debouncer.timer.Stop()
		debouncer.timer = nil
	}
	debouncer.mu.Unlock()
	debouncer.flushTimer(epoch)

	if attempts != 2 {
		t.Fatalf("expected one failed attempt and one retry, got %d", attempts)
	}
	if len(successful) != 1 {
		t.Fatalf("expected one latest token update, got %#v", successful)
	}
	if successful[7].IP != "198.51.100.2" || successful[7].At != 200 {
		t.Fatalf("retry wrote stale token use: %#v", successful[7])
	}
}

func TestTokenUseDebouncerTimerFailureInvalidatesConcurrentNormalTimer(t *testing.T) {
	Resume()
	t.Cleanup(Resume)
	errFlush := errors.New("timer flush failed")
	attempts := 0
	var successful map[uint]Update
	var debouncer *Debouncer
	debouncer = New(time.Hour, func(updates map[uint]Update) error {
		attempts++
		copied := make(map[uint]Update, len(updates))
		for id, update := range updates {
			copied[id] = update
		}
		if attempts == 1 {
			debouncer.Record(8, "198.51.100.8", 300)
			return errFlush
		}
		successful = copied
		return nil
	})
	debouncer.failureBackoff = 2 * time.Hour
	t.Cleanup(func() {
		stopTokenUseDebouncerTimer(debouncer)
	})

	debouncer.Record(7, "198.51.100.1", 100)
	staleEpoch := stopTokenUseDebouncerTimer(debouncer)
	debouncer.flushTimer(staleEpoch)
	if attempts != 1 {
		t.Fatalf("expected first timer write attempt, got %d", attempts)
	}

	debouncer.mu.Lock()
	if debouncer.epoch == staleEpoch {
		t.Fatalf("failed write did not invalidate stale timer epoch %d", staleEpoch)
	}
	if len(debouncer.pending) != 2 {
		t.Fatalf("expected failed and concurrent updates to remain pending: %#v", debouncer.pending)
	}
	if debouncer.timer == nil {
		t.Fatal("expected retry timer after failed write")
	}
	circuitUntil := debouncer.circuitUntil
	debouncer.mu.Unlock()
	if !circuitUntil.After(time.Now()) {
		t.Fatalf("expected open circuit after failed write: %v", circuitUntil)
	}

	debouncer.flushTimer(staleEpoch)
	if attempts != 1 {
		t.Fatalf("stale normal timer callback retried write, attempts=%d", attempts)
	}

	debouncer.Record(9, "198.51.100.9", 400)
	debouncer.mu.Lock()
	if !debouncer.circuitUntil.Equal(circuitUntil) {
		t.Fatalf("Record shortened or changed open circuit: before %v after %v", circuitUntil, debouncer.circuitUntil)
	}
	retryEpoch := debouncer.epoch
	debouncer.mu.Unlock()

	debouncer.flushTimer(retryEpoch)
	if attempts != 1 {
		t.Fatalf("open circuit allowed immediate retry, attempts=%d", attempts)
	}

	debouncer.mu.Lock()
	debouncer.circuitUntil = time.Now().Add(-time.Second)
	retryEpoch = debouncer.epoch
	if debouncer.timer != nil {
		debouncer.timer.Stop()
		debouncer.timer = nil
	}
	debouncer.mu.Unlock()
	debouncer.flushTimer(retryEpoch)
	if attempts != 2 {
		t.Fatalf("expected retry after cooldown, got %d attempts", attempts)
	}
	if len(successful) != 3 {
		t.Fatalf("expected all bounded pending updates on retry, got %#v", successful)
	}
	for id, expected := range map[uint]Update{
		7: {IP: "198.51.100.1", At: 100},
		8: {IP: "198.51.100.8", At: 300},
		9: {IP: "198.51.100.9", At: 400},
	} {
		if successful[id] != expected {
			t.Fatalf("retry update %d = %#v, want %#v", id, successful[id], expected)
		}
	}
}

func TestTokenUseDebouncerManualFlushBypassesCircuit(t *testing.T) {
	Resume()
	t.Cleanup(Resume)
	attempts := 0
	debouncer := New(time.Hour, func(updates map[uint]Update) error {
		attempts++
		if len(updates) != 1 || updates[7].IP != "198.51.100.7" || updates[7].At != 700 {
			t.Fatalf("manual flush wrote unexpected updates: %#v", updates)
		}
		return nil
	})
	t.Cleanup(func() {
		stopTokenUseDebouncerTimer(debouncer)
	})

	debouncer.mu.Lock()
	debouncer.circuitUntil = time.Now().Add(time.Hour)
	debouncer.mu.Unlock()
	debouncer.Record(7, "198.51.100.7", 700)
	if err := debouncer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("manual flush did not bypass open circuit, attempts=%d", attempts)
	}
	debouncer.mu.Lock()
	circuitUntil := debouncer.circuitUntil
	pending := len(debouncer.pending)
	timer := debouncer.timer
	debouncer.mu.Unlock()
	if !circuitUntil.IsZero() {
		t.Fatalf("manual flush success did not close circuit: %v", circuitUntil)
	}
	if pending != 0 {
		t.Fatalf("manual flush left pending updates: %d", pending)
	}
	if timer != nil {
		t.Fatal("manual flush left retry timer after draining pending updates")
	}
}

func TestTokenUseDebouncerForceFlushFailureDoesNotRetry(t *testing.T) {
	Resume()
	t.Cleanup(Resume)
	errFlush := errors.New("force flush failed")
	debouncer := New(time.Hour, func(updates map[uint]Update) error {
		return errFlush
	})
	t.Cleanup(func() {
		stopTokenUseDebouncerTimer(debouncer)
	})

	debouncer.Record(7, "198.51.100.7", 700)
	if err := debouncer.flushNow(context.Background(), true, false); !errors.Is(err, errFlush) {
		t.Fatalf("force flush error = %v, want %v", err, errFlush)
	}
	debouncer.mu.Lock()
	pending := len(debouncer.pending)
	circuitUntil := debouncer.circuitUntil
	timer := debouncer.timer
	debouncer.mu.Unlock()
	if pending != 0 {
		t.Fatalf("force flush failure requeued pending updates: %d", pending)
	}
	if !circuitUntil.IsZero() {
		t.Fatalf("force flush failure opened circuit: %v", circuitUntil)
	}
	if timer != nil {
		t.Fatal("force flush failure scheduled retry timer")
	}
}

func TestTokenUseDebouncerShutdownFlushFailurePreservesPendingUpdate(t *testing.T) {
	Resume()
	t.Cleanup(Resume)
	errFlush := errors.New("shutdown flush failed")
	debouncer := New(time.Hour, func(map[uint]Update) error {
		return errFlush
	})
	t.Cleanup(func() {
		stopTokenUseDebouncerTimer(debouncer)
	})

	debouncer.Record(7, "198.51.100.7", 700)
	if err := debouncer.flushNow(context.Background(), true, true); !errors.Is(err, errFlush) {
		t.Fatalf("shutdown flush error = %v, want %v", err, errFlush)
	}
	debouncer.mu.Lock()
	pending := debouncer.pending[7]
	circuitUntil := debouncer.circuitUntil
	timer := debouncer.timer
	debouncer.mu.Unlock()
	if pending.IP != "198.51.100.7" || pending.At != 700 || circuitUntil.IsZero() || timer == nil {
		t.Fatalf("shutdown failure did not preserve retry state: pending=%#v circuit=%v timer=%v", pending, circuitUntil, timer)
	}
}

func stopTokenUseDebouncerTimer(debouncer *Debouncer) uint64 {
	debouncer.mu.Lock()
	defer debouncer.mu.Unlock()
	if debouncer.timer != nil {
		debouncer.timer.Stop()
		debouncer.timer = nil
	}
	return debouncer.epoch
}
