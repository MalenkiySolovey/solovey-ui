package logger

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type logRingBuffer struct {
	items []bufferedLog
	next  int
	full  bool
}

func newLogRingBuffer(capacity int) *logRingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &logRingBuffer{items: make([]bufferedLog, 0, capacity)}
}

func addToBuffer(source, level, message string) {
	addToBufferAt(source, parseSlogLevel(level), message, time.Now())
}

func addToBufferAt(source string, level slog.Level, message string, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	logBufferMu.Lock()
	defer logBufferMu.Unlock()
	logBuffer.append(bufferedLog{
		time: at.Format("2006/01/02 15:04:05"), at: at,
		level: level, source: source, log: message,
	})
}

func Logs(count int, level string) []string {
	return FilteredLogs(count, level, "", "")
}

func FilteredLogs(count int, level, source, filter string) []string {
	entries := Entries(count, level, source, filter)
	output := make([]string, 0, len(entries))
	for _, entry := range entries {
		output = append(output, fmt.Sprintf("%s %s - %s", entry.Time, entry.Level, entry.Message))
	}
	return output
}

func Entries(count int, level, source, filter string) []Entry {
	output := make([]Entry, 0)
	minLevel := parseSlogLevel(level)
	logBufferMu.RLock()
	snapshot := logBuffer.snapshot()
	logBufferMu.RUnlock()
	for index := len(snapshot) - 1; index >= 0 && len(output) < count; index-- {
		entry := snapshot[index]
		if source != "" && entry.source != source {
			continue
		}
		if filter != "" && !strings.Contains(entry.log, filter) {
			continue
		}
		if entry.level >= minLevel {
			output = append(output, newEntry(entry))
		}
	}
	return output
}

func newEntry(entry bufferedLog) Entry {
	timestamp := int64(0)
	if !entry.at.IsZero() {
		timestamp = entry.at.Unix()
	}
	return Entry{Time: entry.time, Timestamp: timestamp, Level: slogLevelName(entry.level), Source: entry.source, Message: entry.log}
}

func (r *logRingBuffer) append(entry bufferedLog) {
	if cap(r.items) == 0 {
		r.items = make([]bufferedLog, 0, 1)
	}
	if len(r.items) < cap(r.items) {
		r.items = append(r.items, entry)
		if len(r.items) == cap(r.items) {
			r.full = true
			r.next = 0
		}
		return
	}
	r.items[r.next] = entry
	r.next = (r.next + 1) % len(r.items)
	r.full = true
}

func (r *logRingBuffer) snapshot() []bufferedLog {
	if len(r.items) == 0 {
		return nil
	}
	out := make([]bufferedLog, 0, len(r.items))
	if !r.full {
		return append(out, r.items...)
	}
	out = append(out, r.items[r.next:]...)
	out = append(out, r.items[:r.next]...)
	return out
}
