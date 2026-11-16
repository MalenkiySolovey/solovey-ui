//go:build !minimal

package remotesubservice

import (
	"context"
	"sync"
	"time"
)

// Scheduler owns the single admitted source refresh loop. Construction has no
// activity; the composition starts it only for an enabled component.
type Scheduler struct {
	mu   sync.Mutex
	stop context.CancelFunc
	done chan struct{}
}

func (s *Scheduler) Start(ctx context.Context, tick func(context.Context)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		return false
	}
	ticker := time.NewTicker(time.Minute)
	s.startLocked(ctx, ticker.C, ticker.Stop, tick)
	return true
}
func (s *Scheduler) startLocked(ctx context.Context, ticks <-chan time.Time, stopTicker func(), tick func(context.Context)) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.stop = cancel
	s.done = done
	go func() {
		defer close(done)
		defer stopTicker()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticks:
				if ctx.Err() != nil {
					return
				}
				tick(ctx)
			}
		}
	}()
}
func (s *Scheduler) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.mu.Unlock()
	if stop == nil {
		return nil
	}
	stop()
	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done:
		default:
			return ctx.Err()
		}
	}
	s.mu.Lock()
	if s.done == done {
		s.stop = nil
		s.done = nil
	}
	s.mu.Unlock()
	return nil
}
