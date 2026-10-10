package autohttps

import (
	"errors"
	"time"
)

func tighterDeadline(caller, bound time.Time) time.Time {
	if bound.IsZero() || !caller.IsZero() && caller.Before(bound) {
		return caller
	}
	return bound
}

// Deadline requests are serialized with the temporary inspection limits.
// Clearing/extending a caller deadline cannot remove an active phase bound,
// and restoring a phase keeps the latest caller request, including concurrent
// updates. Callers set deadlines through the wrapper after Accept.
func (c *AutoHttpsConn) SetDeadline(value time.Time) error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.readDeadline, c.writeDeadline = value, value
	readErr := c.Conn.SetReadDeadline(tighterDeadline(value, c.readBound))
	writeErr := c.Conn.SetWriteDeadline(tighterDeadline(value, c.writeBound))
	return errors.Join(readErr, writeErr)
}

func (c *AutoHttpsConn) SetReadDeadline(value time.Time) error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.readDeadline = value
	return c.Conn.SetReadDeadline(tighterDeadline(value, c.readBound))
}

func (c *AutoHttpsConn) SetWriteDeadline(value time.Time) error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.writeDeadline = value
	return c.Conn.SetWriteDeadline(tighterDeadline(value, c.writeBound))
}

func (c *AutoHttpsConn) beginReadInspection() error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.readBound = time.Now().Add(firstReadTimeout)
	return c.Conn.SetReadDeadline(tighterDeadline(c.readDeadline, c.readBound))
}

func (c *AutoHttpsConn) endReadInspection() {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.readBound = time.Time{}
	_ = c.Conn.SetReadDeadline(c.readDeadline)
}

func (c *AutoHttpsConn) beginRedirectWrite() error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.writeBound = time.Now().Add(redirectWriteTimeout)
	return c.Conn.SetWriteDeadline(tighterDeadline(c.writeDeadline, c.writeBound))
}

func (c *AutoHttpsConn) endRedirectWrite() {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.writeBound = time.Time{}
	_ = c.Conn.SetWriteDeadline(c.writeDeadline)
}
