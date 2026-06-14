package service

import (
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

const (
	defaultLogCount = 10
	maxLogCount     = 500
	maxLogFilter    = 64
)

type LogQuery struct {
	Count    int
	Level    string
	Source   string
	Filter   string
	Category string
}

type LogEntry struct {
	Time      string   `json:"time"`
	Timestamp int64    `json:"timestamp"`
	Level     string   `json:"level"`
	Source    string   `json:"source"`
	Category  string   `json:"category"`
	Message   string   `json:"message"`
	Hint      string   `json:"hint,omitempty"`
	Signals   []string `json:"signals,omitempty"`
}

type LogInsights struct {
	Total      int            `json:"total"`
	ByLevel    map[string]int `json:"byLevel"`
	ByCategory map[string]int `json:"byCategory"`
	LastError  *LogEntry      `json:"lastError,omitempty"`
}

func (s *ServerService) GetLogs(count string, level string) []string {
	logs, err := s.GetLogsFiltered(count, level, "", "")
	if err != nil {
		return nil
	}
	return logs
}

func (s *ServerService) GetLogsFiltered(count string, level string, source string, filter string) ([]string, error) {
	query, err := ParseLogQuery(count, level, source, filter)
	if err != nil {
		return nil, err
	}
	return logger.GetLogsFiltered(query.Count, query.Level, query.Source, query.Filter), nil
}

func (s *ServerService) GetLogEntriesFiltered(count string, level string, source string, filter string, category string) ([]LogEntry, error) {
	query, err := ParseLogQueryWithCategory(count, level, source, filter, category)
	if err != nil {
		return nil, err
	}

	rawLimit := query.Count
	if query.Category != "" {
		rawLimit = maxLogCount
	}
	rawEntries := logger.GetLogEntriesFiltered(rawLimit, query.Level, query.Source, query.Filter)
	entries := make([]LogEntry, 0, min(query.Count, len(rawEntries)))
	for _, rawEntry := range rawEntries {
		entry := classifyLogEntry(rawEntry)
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

func (s *ServerService) GetLogInsights(count int) LogInsights {
	if count <= 0 {
		count = 200
	}
	entries, err := s.GetLogEntriesFiltered(strconv.Itoa(count), "debug", "", "", "")
	if err != nil {
		return LogInsights{}
	}
	return SummarizeLogEntries(entries)
}

func ParseLogQuery(count string, level string, source string, filter string) (LogQuery, error) {
	return ParseLogQueryWithCategory(count, level, source, filter, "")
}

func ParseLogQueryWithCategory(count string, level string, source string, filter string, category string) (LogQuery, error) {
	parsedCount := defaultLogCount
	if count != "" {
		c, err := strconv.Atoi(count)
		if err != nil || c <= 0 {
			return LogQuery{}, common.NewError("invalid log count")
		}
		if c > maxLogCount {
			c = maxLogCount
		}
		parsedCount = c
	}
	if level == "" {
		level = "debug"
	}
	level = strings.ToLower(level)
	if !isValidLogLevel(level) {
		return LogQuery{}, common.NewError("invalid log level")
	}
	if source != "" && source != "panel" && source != "core" {
		return LogQuery{}, common.NewError("invalid log source")
	}
	if len(filter) > maxLogFilter || containsControlRune(filter) {
		return LogQuery{}, common.NewError("invalid log filter")
	}
	category = strings.ToLower(strings.TrimSpace(category))
	if !isValidLogCategory(category) {
		return LogQuery{}, common.NewError("invalid log category")
	}
	return LogQuery{
		Count:    parsedCount,
		Level:    level,
		Source:   source,
		Filter:   filter,
		Category: category,
	}, nil
}

func isValidLogLevel(level string) bool {
	switch strings.ToLower(level) {
	case "debug", "info", "notice", "warning", "error", "critical":
		return true
	default:
		return false
	}
}

func containsControlRune(value string) bool {
	for _, r := range value {
		if r == 0 || r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func isValidLogCategory(category string) bool {
	switch category {
	case "", "panel", "core", "auth", "subscription", "config", "database", "telegram", "network", "audit", "stats", "backup", "import", "api":
		return true
	default:
		return false
	}
}

func classifyLogEntry(entry logger.Entry) LogEntry {
	message := strings.ToLower(entry.Message)
	category := classifyLogCategory(entry.Source, message)
	signals := classifyLogSignals(message)
	return LogEntry{
		Time:      entry.Time,
		Timestamp: entry.Timestamp,
		Level:     strings.ToLower(entry.Level),
		Source:    entry.Source,
		Category:  category,
		Message:   entry.Message,
		Hint:      logHint(category, message),
		Signals:   signals,
	}
}

func classifyLogCategory(source string, message string) string {
	switch {
	case source == "core":
		return "core"
	case containsAny(message, "login", "password", "session", "csrf", "origin", "token", "scope", "credential"):
		return "auth"
	case containsAny(message, "subscription", "sub ", "sub:", "sub_", "clash", "mihomo"):
		return "subscription"
	case containsAny(message, "sqlite", "database", "wal", "db ", "gorm"):
		return "database"
	case containsAny(message, "telegram", "bot api", "notifier"):
		return "telegram"
	case containsAny(message, "backup", "restore"):
		return "backup"
	case containsAny(message, "import", "x-ui", "xui"):
		return "import"
	case containsAny(message, "audit"):
		return "audit"
	case containsAny(message, "stats", "traffic", "online"):
		return "stats"
	case containsAny(message, "timeout", "connection refused", "network unreachable", "no route", "dial tcp", "tls handshake", "dns lookup"):
		return "network"
	case containsAny(message, "sing-box", "core", "restart", "config", "inbound", "outbound", "endpoint", "route", "rule", "dns", "tls"):
		return "config"
	case containsAny(message, "api", "rate limit", "request"):
		return "api"
	default:
		return "panel"
	}
}

func classifyLogSignals(message string) []string {
	signals := make([]string, 0, 4)
	add := func(signal string, words ...string) {
		if containsAny(message, words...) {
			signals = append(signals, signal)
		}
	}
	add("config_parse", "parse", "json", "unknown field", "missing", "invalid config")
	add("core_restart", "restart", "start service", "core is not running")
	add("network", "timeout", "connection refused", "network unreachable", "no route", "dial tcp", "tls handshake", "dns lookup")
	add("database", "sqlite", "database", "wal", "gorm")
	add("auth", "login", "password", "session", "csrf", "origin", "token", "scope")
	add("subscription", "subscription", "clash", "mihomo")
	return signals
}

func logHint(category string, message string) string {
	switch category {
	case "core":
		if containsAny(message, "parse", "config", "json", "unknown field") {
			return "Generated sing-box config was likely rejected; check the config_parse diagnostics row."
		}
		return "Inspect sing-box runtime status, recent config changes, and core restart result."
	case "config":
		return "Check generated config sections, referenced tags, DNS rules, routes, and recent save/import changes."
	case "auth":
		return "Check login/session/API token settings, browser origin, and recent admin changes."
	case "subscription":
		return "Check subscription settings, client links, Clash/Mihomo export, and sub path/domain."
	case "database":
		return "Check SQLite quick_check, WAL checkpoint messages, disk space, and recent imports/backups."
	case "telegram":
		return "Check Telegram bot token/chat settings, proxy/outbound transport, and Bot API network access."
	case "network":
		return "Check firewall, ports, DNS resolution, TLS handshake, and outbound connectivity."
	case "backup":
		return "Check backup destination, passphrase, excluded tables, and restore/import logs."
	case "import":
		return "Check import source format, rollback result, and post-import config diagnostics."
	case "audit":
		return "Check audit retention, writer queue pressure, and security events."
	case "stats":
		return "Check core stats availability and database writes for traffic counters."
	case "api":
		return "Check API token scope, rate limits, and request parameters."
	default:
		return ""
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func SummarizeLogEntries(entries []LogEntry) LogInsights {
	insights := LogInsights{
		Total:      len(entries),
		ByLevel:    map[string]int{},
		ByCategory: map[string]int{},
	}
	for i := range entries {
		entry := entries[i]
		insights.ByLevel[entry.Level]++
		insights.ByCategory[entry.Category]++
		if insights.LastError == nil && entry.Level == "error" {
			lastError := entry
			insights.LastError = &lastError
		}
	}
	return insights
}
