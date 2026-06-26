package service

import (
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
)

const restartSignalDelay = 3 * time.Second

var inProcessRestart struct {
	mu sync.RWMutex
	fn func()
}

type RestartScheduler interface {
	ScheduleRestart(delay time.Duration) error
}

func init() {
	backup.SetSendSighupHook(func() error {
		manager := DefaultRuntime().restart()
		if manager == nil {
			return nil
		}
		return manager.SendSighup()
	})
}

func SetInProcessRestart(fn func()) {
	inProcessRestart.mu.Lock()
	inProcessRestart.fn = fn
	inProcessRestart.mu.Unlock()
}

func StopRestartManager() {
	manager := DefaultRuntime().restart()
	if manager != nil {
		manager.CancelPending()
	}
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
