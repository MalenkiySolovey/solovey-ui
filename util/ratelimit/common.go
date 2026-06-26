package ratelimit

import (
	"sync"
	"time"
)

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
	Count      int
}

type janitor struct {
	stop chan struct{}
	once sync.Once
}

func startJanitor(interval time.Duration, sweep func(time.Time)) *janitor {
	if interval <= 0 || sweep == nil {
		return nil
	}
	j := &janitor{stop: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				sweep(now)
			case <-j.stop:
				return
			}
		}
	}()
	return j
}

func (j *janitor) close() {
	if j == nil {
		return
	}
	j.once.Do(func() { close(j.stop) })
}

func retryAfter(deadline time.Time, now time.Time, fallback time.Duration) time.Duration {
	retry := deadline.Sub(now)
	if retry <= 0 {
		return fallback
	}
	return retry
}
