package server

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/diagnostics"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

func (s *ServerService) GetLogs(count string, level string) []string {
	logs, err := s.GetLogsFiltered(count, level, "", "")
	if err != nil {
		return nil
	}
	return logs
}

func (s *ServerService) GetLogsFiltered(count string, level string, source string, filter string) ([]string, error) {
	query, err := diagnostics.ParseLogQuery(count, level, source, filter)
	if err != nil {
		return nil, err
	}
	return logger.FilteredLogs(query.Count, query.Level, query.Source, query.Filter), nil
}

func (s *ServerService) GetLogEntriesFiltered(count string, level string, source string, filter string, category string) ([]diagnostics.LogEntry, error) {
	query, err := diagnostics.ParseLogQueryWithCategory(count, level, source, filter, category)
	if err != nil {
		return nil, err
	}

	rawLimit := query.Count
	if query.Category != "" {
		rawLimit = diagnostics.MaxLogCount
	}
	rawEntries := logger.Entries(rawLimit, query.Level, query.Source, query.Filter)
	entries := make([]diagnostics.LogEntry, 0, min(query.Count, len(rawEntries)))
	for _, rawEntry := range rawEntries {
		entry := diagnostics.ClassifyLogEntry(rawEntry)
		if query.Category != "" && entry.Category != query.Category {
			continue
		}
		entries = append(entries, entry)
		if len(entries) >= query.Count {
			break
		}
	}
	return entries, nil
}

func (s *ServerService) GetLogInsights(count int) diagnostics.LogInsights {
	if count <= 0 {
		count = 200
	}
	entries, err := s.GetLogEntriesFiltered(strconv.Itoa(count), "debug", "", "", "")
	if err != nil {
		return diagnostics.LogInsights{}
	}
	return diagnostics.SummarizeLogEntries(entries)
}
