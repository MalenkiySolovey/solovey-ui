//go:build !minimal

package remotesubservice

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
)

const healthSourceBatch = 32
const healthTargetBatch = 128
const healthScheduleLimit = 1024

type sourceHealthCursor struct {
	revision string
	after    uint
	next     time.Time
	touched  time.Time
}

// This is bounded scheduling bookkeeping, not health truth. Observations stay
// in CoreRuntime and expire/reset with the concrete runtime generation.
type HealthSchedule struct {
	after   uint
	entries map[uint]*sourceHealthCursor
}

type SourceHealthTarget struct {
	ID      uint
	Tag     string
	Type    string
	Options json.RawMessage
}

func (s *Service) ProbeDueSubscriptionsContext(ctx context.Context, schedule *HealthSchedule) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if schedule == nil || s.ProbeOutbound == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ctx, release, err := s.Store.AcquireOperation(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	db := s.Store
	read := func(after uint) ([]remotesub.RemoteOutboundSubscription, error) {
		var sources []remotesub.RemoteOutboundSubscription
		err := db.WithContext(ctx).ListHealthSources(after, healthSourceBatch, &sources)
		return sources, err
	}
	sources, err := read(schedule.after)
	if err == nil && len(sources) == 0 && schedule.after != 0 {
		sources, err = read(0)
	}
	if err != nil {
		return 0, err
	}
	if schedule.entries == nil {
		schedule.entries = make(map[uint]*sourceHealthCursor)
	}
	completed := 0
	for _, source := range sources {
		if ctx.Err() != nil {
			break
		}
		schedule.after = source.Id
		policy, err := s.Security.ParseSourceHealthCheck(source.HealthCheck)
		if err != nil || !policy.Enabled {
			continue
		}
		now := time.Now()
		entry := schedule.entries[source.Id]
		if entry == nil || entry.revision != string(source.HealthCheck) {
			entry = &sourceHealthCursor{revision: string(source.HealthCheck)}
			schedule.entries[source.Id] = entry
		}
		entry.touched = now
		if now.Before(entry.next) {
			continue
		}
		var targets []SourceHealthTarget
		if err := db.WithContext(ctx).ListHealthTargets(source.Id, entry.after, healthTargetBatch, &targets); err != nil {
			return completed, err
		}
		prefix, checked := s.probeSourceTargets(ctx, targets, policy)
		completed += checked
		if prefix > 0 {
			entry.after = targets[prefix-1].ID
		}
		if prefix == len(targets) && len(targets) < healthTargetBatch {
			interval, _ := time.ParseDuration(policy.Interval)
			entry.next, entry.after = now.Add(interval), 0
		}
	}
	for len(schedule.entries) > healthScheduleLimit {
		var oldest uint
		for id, entry := range schedule.entries {
			if oldest == 0 || entry.touched.Before(schedule.entries[oldest].touched) {
				oldest = id
			}
		}
		delete(schedule.entries, oldest)
	}
	return completed, nil
}

func (s *Service) probeSourceTargets(ctx context.Context, targets []SourceHealthTarget, policy remotesub.SourceHealthCheck) (int, int) {
	finished := make([]bool, len(targets))
	checked := make([]bool, len(targets))
	jobs := make(chan int)
	var workers sync.WaitGroup
	timeout, _ := time.ParseDuration(policy.Timeout)
	for range min(8, len(targets)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				target := targets[index]
				if s.Security.OutboundAvailable(target.Type, target.Options) {
					checked[index] = true
					s.ProbeOutbound(ctx, target.Tag, policy.URL, timeout)
				}
				finished[index] = ctx.Err() == nil
			}
		}()
	}
dispatch:
	for index := range targets {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	count := 0
	for _, called := range checked {
		if called {
			count++
		}
	}
	for index, done := range finished {
		if !done {
			return index, count
		}
	}
	return len(targets), count
}
