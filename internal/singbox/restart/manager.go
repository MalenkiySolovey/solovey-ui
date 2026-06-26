package restart

import (
	"sync"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

type Manager struct {
	mu           sync.Mutex
	cond         *sync.Cond
	inFlight     bool
	pendingTimer *time.Timer
	signalDelay  time.Duration
	signal       func() error
}

func NewManager(signalDelay time.Duration, signal func() error) *Manager {
	manager := &Manager{
		signalDelay: signalDelay,
		signal:      signal,
	}
	manager.cond = sync.NewCond(&manager.mu)
	return manager
}

func (m *Manager) Run(operation func() error) error {
	if !m.begin() {
		return nil
	}
	defer m.end()
	return operation()
}

// RunBlocking waits for any in-flight restart/apply operation and then runs
// the operation exclusively. Use it when silently skipping would leave the
// running core out of sync with committed state.
func (m *Manager) RunBlocking(operation func() error) error {
	m.beginBlocking()
	defer m.end()
	return operation()
}

func (m *Manager) SendSighup() error {
	return m.ScheduleRestart(m.signalDelay)
}

func (m *Manager) ScheduleRestart(delay time.Duration) error {
	if delay <= 0 {
		delay = m.signalDelay
	}
	if !m.begin() {
		return nil
	}
	m.armRestartTimer(delay)
	return nil
}

// ScheduleRestartBlocking guarantees that a restart is pending before it
// returns. An existing pending restart is sufficient; another kind of in-flight
// operation is allowed to finish before a new timer is armed.
func (m *Manager) ScheduleRestartBlocking(delay time.Duration) error {
	if delay <= 0 {
		delay = m.signalDelay
	}
	m.mu.Lock()
	for m.inFlight {
		if m.pendingTimer != nil {
			m.mu.Unlock()
			return nil
		}
		m.cond.Wait()
	}
	m.inFlight = true
	m.mu.Unlock()
	m.armRestartTimer(delay)
	return nil
}

func (m *Manager) armRestartTimer(delay time.Duration) {
	m.mu.Lock()
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		m.mu.Lock()
		self := timer
		m.mu.Unlock()
		defer m.endPending(self)
		if err := m.signal(); err != nil {
			logger.Error("send signal SIGHUP failed:", err)
		}
	})
	m.pendingTimer = timer
	m.mu.Unlock()
}

func (m *Manager) CancelPending() {
	m.mu.Lock()
	timer := m.pendingTimer
	if timer == nil {
		m.mu.Unlock()
		return
	}
	m.pendingTimer = nil
	if timer.Stop() {
		m.inFlight = false
		m.cond.Broadcast()
	}
	m.mu.Unlock()
}

func (m *Manager) begin() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight {
		return false
	}
	m.inFlight = true
	return true
}

func (m *Manager) beginBlocking() {
	m.mu.Lock()
	for m.inFlight {
		m.cond.Wait()
	}
	m.inFlight = true
	m.mu.Unlock()
}

func (m *Manager) end() {
	m.mu.Lock()
	m.inFlight = false
	m.cond.Broadcast()
	m.mu.Unlock()
}

func (m *Manager) endPending(timer *time.Timer) {
	m.mu.Lock()
	if m.pendingTimer == timer {
		m.pendingTimer = nil
	}
	m.inFlight = false
	m.cond.Broadcast()
	m.mu.Unlock()
}
