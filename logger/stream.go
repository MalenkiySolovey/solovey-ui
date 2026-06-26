package logger

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

type streamBackend struct {
	writer      io.Writer
	includeTime bool
	mu          sync.Mutex
}

func newStreamBackend(writer io.Writer, includeTime bool) *streamBackend {
	return &streamBackend{writer: writer, includeTime: includeTime}
}

func (b *streamBackend) Log(at time.Time, level slog.Level, message string) {
	if at.IsZero() {
		at = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.includeTime {
		fmt.Fprintf(b.writer, "%s %s - %s\n", at.Format("2006/01/02 15:04:05"), slogLevelName(level), message)
		return
	}
	fmt.Fprintf(b.writer, "%s - %s\n", slogLevelName(level), message)
}
