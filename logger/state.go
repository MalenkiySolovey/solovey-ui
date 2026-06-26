package logger

import (
	"log/slog"
	"sync"
	"time"
)

const logBufferCapacity = 10240

type Level string

const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

type Entry struct {
	Time      string `json:"time"`
	Timestamp int64  `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
}

type bufferedLog struct {
	time   string
	at     time.Time
	level  slog.Level
	source string
	log    string
}

type loggerConfig struct {
	backend  logBackend
	minLevel slog.Level
}

type logBackend interface {
	Log(t time.Time, level slog.Level, message string)
}

var (
	logConfigMu sync.RWMutex
	logConfig   = loggerConfig{minLevel: slog.LevelDebug}
	logBufferMu sync.RWMutex
	logBuffer   = newLogRingBuffer(logBufferCapacity)
)
