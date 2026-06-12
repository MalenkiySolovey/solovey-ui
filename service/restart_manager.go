package service

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

const restartSignalDelay = 3 * time.Second

var inProcessRestart struct {
	mu sync.RWMutex
	fn func()
}

type restartManager struct {
	mu           sync.Mutex
	inFlight     bool
	pendingTimer *time.Timer
	signalDelay  time.Duration
	signal       func() error
}

type RestartScheduler interface {
	ScheduleRestart(delay time.Duration) error
}

func init() {
	database.SetSendSighupHook(func() error {
		manager := DefaultRuntime().restart()
		if manager == nil {
			return nil
		}
		return manager.sendSighup()
	})
}

func SetInProcessRestart(fn func()) {
	inProcessRestart.mu.Lock()
	inProcessRestart.fn = fn
	inProcessRestart.mu.Unlock()
}

func newRestartManager(signalDelay time.Duration, signal func() error) *restartManager {
	return &restartManager{
		signalDelay: signalDelay,
		signal:      signal,
	}
}

func StopRestartManager() {
	manager := DefaultRuntime().restart()
	if manager != nil {
		manager.cancelPending()
	}
}

func (m *restartManager) run(operation func() error) error {
	if !m.begin() {
		return nil
	}
	defer m.end()
	return operation()
}

func (m *restartManager) sendSighup() error {
	return m.ScheduleRestart(m.signalDelay)
}

func (m *restartManager) ScheduleRestart(delay time.Duration) error {
	if delay <= 0 {
		delay = m.signalDelay
	}
	if !m.begin() {
		return nil
	}

	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		defer m.endPending(timer)
		if err := m.signal(); err != nil {
			logger.Error("send signal SIGHUP failed:", err)
		}
	})

	m.mu.Lock()
	m.pendingTimer = timer
	m.mu.Unlock()
	return nil
}

func (m *restartManager) cancelPending() {
	m.mu.Lock()
	timer := m.pendingTimer
	if timer == nil {
		m.mu.Unlock()
		return
	}
	m.pendingTimer = nil
	if timer.Stop() {
		m.inFlight = false
	}
	m.mu.Unlock()
}

func (m *restartManager) begin() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight {
		return false
	}
	m.inFlight = true
	return true
}

func (m *restartManager) end() {
	m.mu.Lock()
	m.inFlight = false
	m.mu.Unlock()
}

func (m *restartManager) endPending(timer *time.Timer) {
	m.mu.Lock()
	if m.pendingTimer == timer {
		m.pendingTimer = nil
	}
	m.inFlight = false
	m.mu.Unlock()
}

func signalCurrentProcess() error {
	if runtime.GOOS == "windows" && runInProcessRestart() {
		return nil
	}
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return process.Signal(syscall.SIGHUP)
}

func runInProcessRestart() bool {
	inProcessRestart.mu.RLock()
	fn := inProcessRestart.fn
	inProcessRestart.mu.RUnlock()
	if fn == nil {
		return false
	}
	fn()
	return true
}
