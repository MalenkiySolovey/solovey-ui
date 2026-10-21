package tracker

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
	entitytypes "github.com/MalenkiySolovey/solovey-ui/internal/entities/types"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

type Counter struct {
	read  *atomic.Int64
	write *atomic.Int64
}

// A display name is an observation label, not a durable accounting key.
type userCounterKey struct {
	name string
	id   entitytypes.VPNUserID
}

type StatsTracker struct {
	access    sync.Mutex
	inbounds  map[string]Counter
	outbounds map[string]Counter
	users     map[userCounterKey]Counter
	inflight  *trackerWaitGroup
	observer  IPObserver
}

func NewStatsTracker(observers ...IPObserver) *StatsTracker {
	tracker := &StatsTracker{
		inbounds:  make(map[string]Counter),
		outbounds: make(map[string]Counter),
		users:     make(map[userCounterKey]Counter),
		inflight:  newTrackerWaitGroup(),
	}
	if len(observers) > 0 {
		tracker.observer = observers[0]
	}
	return tracker
}

func (c *StatsTracker) Reset() {
	c.access.Lock()
	resetCounters(c.inbounds)
	resetCounters(c.outbounds)
	resetCounters(c.users)
	waitGroup := c.inflight
	c.inflight = newTrackerWaitGroup()
	c.access.Unlock()
	waitForTrackerIdle("stats tracker", waitGroup, trackerResetWaitTimeout)
}

func resetCounters[K comparable](counters map[K]Counter) {
	for _, counter := range counters {
		counter.read.Store(0)
		counter.write.Store(0)
	}
}

func (c *StatsTracker) getTrackedReadCounters(inbound string, outbound string, user string, userID entitytypes.VPNUserID) ([]*atomic.Int64, []*atomic.Int64, *trackerWaitGroup) {
	c.access.Lock()
	defer c.access.Unlock()
	readCounter, writeCounter := c.getReadCountersLocked(inbound, outbound, user, userID)
	c.inflight.Add()
	return readCounter, writeCounter, c.inflight
}

func (c *StatsTracker) getReadCountersLocked(inbound string, outbound string, user string, userID entitytypes.VPNUserID) ([]*atomic.Int64, []*atomic.Int64) {
	var readCounter []*atomic.Int64
	var writeCounter []*atomic.Int64

	if inbound != "" {
		readCounter = append(readCounter, loadOrCreateCounter(&c.inbounds, inbound).read)
		writeCounter = append(writeCounter, c.inbounds[inbound].write)
	}
	if outbound != "" {
		readCounter = append(readCounter, loadOrCreateCounter(&c.outbounds, outbound).read)
		writeCounter = append(writeCounter, c.outbounds[outbound].write)
	}
	if user != "" {
		readCounter = append(readCounter, loadOrCreateCounter(&c.users, userCounterKey{name: user, id: userID}).read)
		writeCounter = append(writeCounter, c.users[userCounterKey{name: user, id: userID}].write)
	}
	return readCounter, writeCounter
}

func loadOrCreateCounter[K comparable](obj *map[K]Counter, name K) Counter {
	counter, loaded := (*obj)[name]
	if loaded {
		return counter
	}
	counter = Counter{read: &atomic.Int64{}, write: &atomic.Int64{}}
	(*obj)[name] = counter
	return counter
}

func (c *StatsTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	result := conn
	if !inboundidentity.Admission(ctx, func() { result, _ = c.admitConnection(ctx, conn, metadata, matchOutbound) }) {
		_ = conn.Close()
	}
	return result
}

func (c *StatsTracker) admitConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchOutbound adapter.Outbound) (net.Conn, bool) {
	sourceIP := sourceIPFromMetadata(metadata)
	if c.observer != nil && !c.observer.ObserveAndAllow(metadata.User, sourceIP) {
		_ = conn.Close()
		return conn, false
	}
	outbound := ""
	if matchOutbound != nil {
		outbound = matchOutbound.Tag()
	}
	readCounter, writeCounter, waitGroup := c.getTrackedReadCounters(metadata.Inbound, outbound, metadata.User, inboundidentity.AccountingUserID(ctx, metadata))
	return newStatsTrackedConn(bufio.NewInt64CounterConn(conn, readCounter, writeCounter), waitGroup), true
}

func (c *StatsTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	result := conn
	if !inboundidentity.Admission(ctx, func() { result, _ = c.admitPacketConnection(ctx, conn, metadata, matchOutbound) }) {
		_ = conn.Close()
	}
	return result
}

func (c *StatsTracker) admitPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchOutbound adapter.Outbound) (network.PacketConn, bool) {
	sourceIP := sourceIPFromMetadata(metadata)
	if c.observer != nil && !c.observer.ObserveAndAllow(metadata.User, sourceIP) {
		_ = conn.Close()
		return conn, false
	}
	outbound := ""
	if matchOutbound != nil {
		outbound = matchOutbound.Tag()
	}
	readCounter, writeCounter, waitGroup := c.getTrackedReadCounters(metadata.Inbound, outbound, metadata.User, inboundidentity.AccountingUserID(ctx, metadata))
	return newStatsTrackedPacketConn(bufio.NewInt64CounterPacketConn(conn, readCounter, nil, writeCounter, nil), waitGroup), true
}

func newStatsTrackedConn(conn net.Conn, waitGroup *trackerWaitGroup) net.Conn {
	return &statsTrackedConn{
		Conn:      conn,
		waitGroup: waitGroup,
	}
}

type statsTrackedConn struct {
	net.Conn
	waitGroup *trackerWaitGroup
	doneOnce  sync.Once
}

func (w *statsTrackedConn) done() {
	w.doneOnce.Do(func() {
		w.waitGroup.Done()
	})
}

func (w *statsTrackedConn) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if shouldUntrackIOErr(err) {
		w.done()
	}
	return n, err
}

func (w *statsTrackedConn) Write(b []byte) (int, error) {
	n, err := w.Conn.Write(b)
	if err != nil && shouldUntrackIOErr(err) {
		w.done()
	}
	return n, err
}

func (w *statsTrackedConn) Close() error {
	w.done()
	return w.Conn.Close()
}

func (w *statsTrackedConn) Upstream() any {
	return w.Conn
}

func newStatsTrackedPacketConn(conn network.PacketConn, waitGroup *trackerWaitGroup) network.PacketConn {
	return &statsTrackedPacketConn{
		PacketConn: conn,
		waitGroup:  waitGroup,
	}
}

type statsTrackedPacketConn struct {
	network.PacketConn
	waitGroup *trackerWaitGroup
	doneOnce  sync.Once
}

func (w *statsTrackedPacketConn) done() {
	w.doneOnce.Do(func() {
		w.waitGroup.Done()
	})
}

func (w *statsTrackedPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	dest, err := w.PacketConn.ReadPacket(buffer)
	if shouldUntrackIOErr(err) {
		w.done()
	}
	return dest, err
}

func (w *statsTrackedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	err := w.PacketConn.WritePacket(buffer, destination)
	if err != nil && shouldUntrackIOErr(err) {
		w.done()
	}
	return err
}

func (w *statsTrackedPacketConn) Close() error {
	w.done()
	return w.PacketConn.Close()
}

func (w *statsTrackedPacketConn) Upstream() any {
	return w.PacketConn
}

func sourceIPFromMetadata(metadata adapter.InboundContext) string {
	if metadata.Source.Addr.IsValid() {
		return metadata.Source.Addr.String()
	}
	return ""
}

type Stat struct {
	// VPNUserID is captured at authenticated admission, never resolved by name
	// at flush time. Zero denotes an observation without durable attribution.
	VPNUserID entitytypes.VPNUserID
	DateTime  int64
	Resource  string
	Tag       string
	Direction bool
	Traffic   int64
}

func (c *StatsTracker) GetStats() []Stat {
	c.access.Lock()
	defer c.access.Unlock()

	dt := time.Now().Unix()

	s := []Stat{}
	for inbound, counter := range c.inbounds {
		down := counter.write.Swap(0)
		up := counter.read.Swap(0)
		if down > 0 || up > 0 {
			s = append(s, Stat{
				DateTime:  dt,
				Resource:  "inbound",
				Tag:       inbound,
				Direction: false,
				Traffic:   down,
			}, Stat{
				DateTime:  dt,
				Resource:  "inbound",
				Tag:       inbound,
				Direction: true,
				Traffic:   up,
			})
		}
	}

	for outbound, counter := range c.outbounds {
		down := counter.write.Swap(0)
		up := counter.read.Swap(0)
		if down > 0 || up > 0 {
			s = append(s, Stat{
				DateTime:  dt,
				Resource:  "outbound",
				Tag:       outbound,
				Direction: false,
				Traffic:   down,
			}, Stat{
				DateTime:  dt,
				Resource:  "outbound",
				Tag:       outbound,
				Direction: true,
				Traffic:   up,
			})
		}
	}

	for user, counter := range c.users {
		down := counter.write.Swap(0)
		up := counter.read.Swap(0)
		if down > 0 || up > 0 {
			s = append(s, Stat{
				DateTime:  dt,
				Resource:  "user",
				Tag:       user.name,
				VPNUserID: user.id,
				Direction: false,
				Traffic:   down,
			}, Stat{
				DateTime:  dt,
				Resource:  "user",
				Tag:       user.name,
				VPNUserID: user.id,
				Direction: true,
				Traffic:   up,
			})
		}
	}
	return s
}

// RestoreStats returns samples drained by GetStats after their persistence
// transaction fails. Counters are incremented rather than replaced so traffic
// observed concurrently with the failed transaction is preserved.
func (c *StatsTracker) RestoreStats(samples []Stat) {
	if len(samples) == 0 {
		return
	}
	c.access.Lock()
	defer c.access.Unlock()
	for _, sample := range samples {
		var counters *map[string]Counter
		switch sample.Resource {
		case "inbound":
			counters = &c.inbounds
		case "outbound":
			counters = &c.outbounds
		case "user":
			counter := loadOrCreateCounter(&c.users, userCounterKey{name: sample.Tag, id: sample.VPNUserID})
			if sample.Direction {
				counter.read.Add(sample.Traffic)
			} else {
				counter.write.Add(sample.Traffic)
			}
			continue
		default:
			continue
		}
		counter := loadOrCreateCounter(counters, sample.Tag)
		if sample.Direction {
			counter.read.Add(sample.Traffic)
		} else {
			counter.write.Add(sample.Traffic)
		}
	}
}
