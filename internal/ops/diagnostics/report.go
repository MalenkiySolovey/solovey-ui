package diagnostics

import (
	"encoding/json"
	"regexp"
)

type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Check struct {
	Key     string         `json:"key"`
	Title   string         `json:"title"`
	Status  Status         `json:"status"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Health struct {
	Status string         `json:"status"`
	Counts map[string]int `json:"counts"`
}

var (
	bearerPattern = regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[^\s,;]+`)
	secretPattern = regexp.MustCompile(`(?i)((?:token|secret|password|passphrase|privatekey|private_key|api[_-]?key)\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;]+)`)
	queryPattern  = regexp.MustCompile(`(?i)([?&](?:token|secret|password|passphrase|api[_-]?key)=)[^&\s]+`)
)

func SummarizeChecks(checks []Check) Health {
	counts := map[string]int{
		string(StatusOK):   0,
		string(StatusWarn): 0,
		string(StatusFail): 0,
	}
	for _, check := range checks {
		counts[string(check.Status)]++
	}
	status := "healthy"
	if counts[string(StatusFail)] > 0 {
		status = "down"
	} else if counts[string(StatusWarn)] > 0 {
		status = "degraded"
	}
	return Health{Status: status, Counts: counts}
}

func RedactText(value string) string {
	value = bearerPattern.ReplaceAllString(value, `${1}[redacted]`)
	value = secretPattern.ReplaceAllString(value, `${1}[redacted]`)
	return queryPattern.ReplaceAllString(value, `${1}[redacted]`)
}

func RedactLogLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, RedactText(line))
	}
	return out
}

func RedactLogEntries(entries []LogEntry) []LogEntry {
	if len(entries) == 0 {
		return entries
	}
	out := make([]LogEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Message = RedactText(entry.Message)
		out = append(out, entry)
	}
	return out
}

func NaiveSupportCheck(raw []byte, supportsNaive bool) []Check {
	if supportsNaive {
		return nil
	}
	count := CountType(raw, "naive")
	if count == 0 {
		return nil
	}
	return []Check{{
		Key:     "naive_outbound_build_tag",
		Title:   "Naive outbound",
		Status:  StatusWarn,
		Message: "generated config uses naive, but this binary was built without with_naive_outbound",
		Details: map[string]any{
			"count": count,
			"fix":   "build/release with -tags with_naive_outbound and the required cronet-go toolchain",
		},
	}}
}

func CountType(raw []byte, outboundType string) int {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0
	}
	count := 0
	for _, section := range []string{"inbounds", "outbounds"} {
		var rows []map[string]any
		if err := json.Unmarshal(doc[section], &rows); err != nil {
			continue
		}
		for _, row := range rows {
			if row["type"] == outboundType {
				count++
			}
		}
	}
	return count
}

func ConfigDetails(raw []byte) map[string]any {
	var doc map[string]json.RawMessage
	details := map[string]any{"bytes": len(raw)}
	if err := json.Unmarshal(raw, &doc); err != nil {
		details["json"] = err.Error()
		return details
	}
	for _, section := range []string{"inbounds", "outbounds", "services", "endpoints"} {
		if rawSection, ok := doc[section]; ok {
			var arr []json.RawMessage
			if err := json.Unmarshal(rawSection, &arr); err == nil {
				details[section] = len(arr)
			}
		}
	}
	for _, section := range []string{"dns", "route", "log", "experimental"} {
		_, ok := doc[section]
		details[section] = ok
	}
	return details
}
