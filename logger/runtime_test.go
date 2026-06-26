package logger

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestLogRingBufferOverflowKeepsNewestEntries(t *testing.T) {
	resetLogBufferForTest(t)

	for i := 0; i < logBufferCapacity+5; i++ {
		addToBuffer("panel", "INFO", fmt.Sprintf("msg-%05d", i))
	}

	logs := FilteredLogs(logBufferCapacity+100, "DEBUG", "", "")
	if len(logs) != logBufferCapacity {
		t.Fatalf("expected %d logs, got %d", logBufferCapacity, len(logs))
	}
	if !strings.Contains(logs[0], "msg-10244") {
		t.Fatalf("newest log was not first: %q", logs[0])
	}
	if !strings.Contains(logs[len(logs)-1], "msg-00005") {
		t.Fatalf("oldest retained log mismatch: %q", logs[len(logs)-1])
	}
}

func TestLogRingBufferConcurrentReadWrite(t *testing.T) {
	resetLogBufferForTest(t)

	const writers = 8
	const readers = 8
	const perWriter = 500
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				addToBuffer("panel", "INFO", fmt.Sprintf("writer-%02d-%03d", id, i))
			}
		}(writer)
	}
	for reader := 0; reader < readers; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_ = FilteredLogs(100, "DEBUG", "panel", "writer-")
			}
		}()
	}
	wg.Wait()

	if logs := FilteredLogs(logBufferCapacity, "DEBUG", "panel", "writer-"); len(logs) == 0 {
		t.Fatal("expected concurrent writes to be visible")
	}
}

func TestSlogAdapterWritesRingBuffer(t *testing.T) {
	resetLogBufferForTest(t)

	Slog("p3").Warn("adapter ready", slog.String("component", "logger"))

	logs := FilteredLogs(10, "DEBUG", "p3", "adapter ready")
	if len(logs) != 1 {
		t.Fatalf("expected one slog-backed entry, got %#v", logs)
	}
	if !strings.Contains(logs[0], "component=logger") {
		t.Fatalf("slog attrs were not formatted: %q", logs[0])
	}
}

func TestLogEntriesFilteredIncludeMetadata(t *testing.T) {
	resetLogBufferForTest(t)

	Slog("panel").Error("metadata ready", slog.String("component", "logger"))

	entries := Entries(10, "debug", "panel", "metadata ready")
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %#v", entries)
	}
	if entries[0].Level != "ERROR" || entries[0].Source != "panel" || entries[0].Timestamp == 0 {
		t.Fatalf("unexpected entry metadata: %#v", entries[0])
	}
	if !strings.Contains(entries[0].Message, "component=logger") {
		t.Fatalf("slog attrs were not preserved: %#v", entries[0])
	}
}

func TestInitInstallsSlogDefault(t *testing.T) {
	resetLogBufferForTest(t)

	Init(LevelDebug)
	slog.Default().Info("default slog ready", slog.String("phase", "p4"))

	logs := FilteredLogs(10, "DEBUG", "panel", "default slog ready")
	if len(logs) != 1 {
		t.Fatalf("expected default slog entry, got %#v", logs)
	}
	if !strings.Contains(logs[0], "phase=p4") {
		t.Fatalf("default slog attrs were not formatted: %q", logs[0])
	}
}

func BenchmarkLogRingBufferAppendOverflow(b *testing.B) {
	resetLogBufferForBenchmark(b)

	for i := 0; i < b.N; i++ {
		addToBuffer("panel", "INFO", "benchmark")
	}
}

func resetLogBufferForTest(t *testing.T) {
	t.Helper()
	logBufferMu.Lock()
	oldBuffer := logBuffer
	logBuffer = newLogRingBuffer(logBufferCapacity)
	logBufferMu.Unlock()
	t.Cleanup(func() {
		logBufferMu.Lock()
		logBuffer = oldBuffer
		logBufferMu.Unlock()
	})
}

func resetLogBufferForBenchmark(b *testing.B) {
	b.Helper()
	logBufferMu.Lock()
	oldBuffer := logBuffer
	logBuffer = newLogRingBuffer(logBufferCapacity)
	logBufferMu.Unlock()
	b.Cleanup(func() {
		logBufferMu.Lock()
		logBuffer = oldBuffer
		logBufferMu.Unlock()
	})
}
