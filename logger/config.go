package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

func Init(level Level) {
	backend := initBackend()
	panelLogger := Slog("panel")
	logConfigMu.Lock()
	logConfig = loggerConfig{backend: backend, minLevel: levelToSlog(level)}
	logConfigMu.Unlock()
	slog.SetDefault(panelLogger)
}

func currentLogConfig() (logBackend, slog.Level) {
	logConfigMu.RLock()
	backend := logConfig.backend
	minLevel := logConfig.minLevel
	logConfigMu.RUnlock()
	if backend == nil {
		backend = newStreamBackend(os.Stdout, false)
	}
	return backend, minLevel
}

func initBackend() logBackend {
	_, inContainer := os.LookupEnv("container")
	if !inContainer {
		_, statErr := os.Stat("/.dockerenv")
		inContainer = statErr == nil
	}
	if inContainer {
		return newStreamBackend(os.Stderr, true)
	}
	backend, err := newSyslogBackend()
	if err == nil {
		return backend
	}
	fmt.Println("Unable to use syslog: " + err.Error())
	return newStreamBackend(os.Stderr, true)
}

func parseSlogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warning", "warn":
		return slog.LevelWarn
	case "error", "critical":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func levelToSlog(level Level) slog.Level {
	switch level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarning:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
