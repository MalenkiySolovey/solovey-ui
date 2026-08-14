package runtime

import "github.com/MalenkiySolovey/solovey-ui/core/tracker"

func (c *Core) IsRunning() bool {
	c.access.RLock()
	defer c.access.RUnlock()
	return c.isRunning
}

func (c *Core) Uptime() (uint32, bool) {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	c.access.RLock()
	instance := c.instance
	running := c.isRunning
	c.access.RUnlock()
	if !running || instance == nil {
		return 0, false
	}
	return instance.Uptime(), true
}

// ConsumeStats atomically drains the runtime counters and keeps their owning
// runtime generation alive until consume completes. Failed consumption is
// restored to the same tracker so a concurrent Stop cannot lose accounting.
func (c *Core) ConsumeStats(consume func([]tracker.Stat) error) (bool, error) {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	c.access.RLock()
	stats := c.statsTracker
	running := c.isRunning
	c.access.RUnlock()
	if !running || stats == nil {
		return false, nil
	}
	samples := stats.GetStats()
	if err := consume(samples); err != nil {
		stats.RestoreStats(samples)
		return true, err
	}
	return true, nil
}

func (c *Core) CloseInboundConnections(tag string) {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	c.access.RLock()
	connections := c.connTracker
	running := c.isRunning
	c.access.RUnlock()
	if running && connections != nil {
		connections.CloseConnByInbound(tag)
	}
}
