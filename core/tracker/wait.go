package tracker

import (
	stdatomic "sync/atomic"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const trackerResetWaitTimeout = 5 * time.Second

type trackerWaitGroup struct {
	active stdatomic.Int64
}

func newTrackerWaitGroup() *trackerWaitGroup {
	return &trackerWaitGroup{}
}

func (g *trackerWaitGroup) Add() {
	g.active.Add(1)
}

func (g *trackerWaitGroup) Done() {
	g.active.Add(-1)
}

func (g *trackerWaitGroup) Active() int64 {
	return g.active.Load()
}

func waitForTrackerIdle(name string, group *trackerWaitGroup, timeout time.Duration) {
	if group == nil {
		return
	}
	deadline := time.Now().Add(timeout)
	for group.Active() != 0 {
		if time.Now().After(deadline) {
			logger.Warningf("%s reset timed out waiting for %d active wrapped connections", name, group.Active())
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
