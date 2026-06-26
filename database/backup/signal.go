package backup

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

var sendSighupHook func() error

var (
	sighupTimeout     time.Duration
	sighupTimeoutOnce sync.Once
)

func SetSendSighupHook(hook func() error) {
	sendSighupHook = hook
}

func resolvedSighupTimeout() time.Duration {
	sighupTimeoutOnce.Do(func() { sighupTimeout = parseSighupTimeoutEnv() })
	return sighupTimeout
}

func parseSighupTimeoutEnv() time.Duration {
	const defaultTimeout = 3 * time.Second
	raw := strings.TrimSpace(os.Getenv("SUI_SIGHUP_TIMEOUT_SECONDS"))
	if raw == "" {
		return defaultTimeout
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > 60 {
		logger.Warning("invalid SUI_SIGHUP_TIMEOUT_SECONDS=", raw, ", falling back to 3s")
		return defaultTimeout
	}
	return time.Duration(parsed) * time.Second
}

func SetSighupTimeoutForTest(timeout time.Duration) {
	sighupTimeout = timeout
	sighupTimeoutOnce.Do(func() {})
}

func SendSighup() error {
	if sendSighupHook != nil {
		return sendSighupHook()
	}
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	time.AfterFunc(resolvedSighupTimeout(), func() {
		var signalErr error
		if runtime.GOOS == "windows" {
			signalErr = process.Kill()
		} else {
			signalErr = process.Signal(syscall.SIGHUP)
		}
		if signalErr != nil {
			logger.Error("send signal SIGHUP failed:", signalErr)
		}
	})
	return nil
}
