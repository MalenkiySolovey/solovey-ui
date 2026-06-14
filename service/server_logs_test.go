package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/logger"
)

func TestGetLogsFilteredValidatesAndFilters(t *testing.T) {
	logger.Init(logger.LevelDebug)
	marker := fmt.Sprintf("f32-%d", time.Now().UnixNano())
	logger.Info(marker, " panel")
	logger.CoreInfo(marker, " core")

	logs, err := (&ServerService{}).GetLogsFiltered("1000", "DEBUG", "core", marker)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one core log, got %#v", logs)
	}
	if !strings.Contains(logs[0], "core") || strings.Contains(logs[0], "panel") {
		t.Fatalf("unexpected filtered log: %#v", logs)
	}

	query, err := ParseLogQuery("1000", "INFO", "panel", "")
	if err != nil {
		t.Fatal(err)
	}
	if query.Count != maxLogCount || query.Level != "info" {
		t.Fatalf("unexpected parsed query: %#v", query)
	}
}

func TestGetLogsFilteredRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		count  string
		level  string
		source string
		filter string
	}{
		{name: "count", count: "0"},
		{name: "level", level: "trace"},
		{name: "source", source: "kernel"},
		{name: "filter length", filter: strings.Repeat("a", maxLogFilter+1)},
		{name: "filter control", filter: "bad\nfilter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&ServerService{}).GetLogsFiltered(tt.count, tt.level, tt.source, tt.filter); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestGetLogEntriesClassifiesAndFiltersCategory(t *testing.T) {
	logger.Init(logger.LevelDebug)
	marker := fmt.Sprintf("f33-%d", time.Now().UnixNano())
	logger.Warning(marker, " login failed for admin")
	logger.CoreError(marker, " sing-box config parse failed")

	entries, err := (&ServerService{}).GetLogEntriesFiltered("100", "debug", "", marker, "auth")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one auth log, got %#v", entries)
	}
	if entries[0].Category != "auth" || entries[0].Level != "warning" || entries[0].Hint == "" {
		t.Fatalf("unexpected classified auth log: %#v", entries[0])
	}

	coreEntries, err := (&ServerService{}).GetLogEntriesFiltered("100", "debug", "core", marker, "core")
	if err != nil {
		t.Fatal(err)
	}
	if len(coreEntries) != 1 || coreEntries[0].Category != "core" {
		t.Fatalf("expected classified core log, got %#v", coreEntries)
	}
	if !containsString(coreEntries[0].Signals, "config_parse") {
		t.Fatalf("expected config_parse signal, got %#v", coreEntries[0].Signals)
	}
}

func TestGetLogEntriesCategoryFilterAppliesBeforeLimit(t *testing.T) {
	logger.Init(logger.LevelDebug)
	marker := fmt.Sprintf("f34-%d", time.Now().UnixNano())
	logger.Warning(marker, " login failed for admin")
	logger.CoreError(marker, " sing-box config parse failed")

	entries, err := (&ServerService{}).GetLogEntriesFiltered("1", "debug", "", marker, "auth")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected older auth log to survive category filtering, got %#v", entries)
	}
	if entries[0].Category != "auth" {
		t.Fatalf("unexpected category: %#v", entries[0])
	}
}

func TestParseLogQueryRejectsInvalidCategory(t *testing.T) {
	if _, err := ParseLogQueryWithCategory("10", "debug", "", "", "kernel"); err == nil {
		t.Fatal("expected invalid category")
	}
}
