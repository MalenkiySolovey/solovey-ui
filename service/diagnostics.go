package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/config"
	"github.com/MalenkiySolovey/solovey-ui/core"
	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"github.com/sagernet/sing-box/option"
)

type DiagnosticStatus string

const (
	DiagnosticStatusOK   DiagnosticStatus = "ok"
	DiagnosticStatusWarn DiagnosticStatus = "warn"
	DiagnosticStatusFail DiagnosticStatus = "fail"
)

type DiagnosticCheck struct {
	Key     string                 `json:"key"`
	Title   string                 `json:"title"`
	Status  DiagnosticStatus       `json:"status"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

type DiagnosticsHealth struct {
	Status string         `json:"status"`
	Counts map[string]int `json:"counts"`
}

type DiagnosticsReport struct {
	GeneratedAt int64                  `json:"generatedAt"`
	Health      DiagnosticsHealth      `json:"health"`
	Checks      []DiagnosticCheck      `json:"checks"`
	System      map[string]interface{} `json:"system"`
	Database    map[string]int64       `json:"database,omitempty"`
	Settings    map[string]interface{} `json:"settings,omitempty"`
	Logs        map[string][]string    `json:"logs"`
	LogInsights LogInsights            `json:"logInsights"`
}

type DiagnosticsBundle struct {
	GeneratedAt int64             `json:"generatedAt"`
	Report      DiagnosticsReport `json:"report"`
	Logs        []LogEntry        `json:"logs"`
	Notes       []string          `json:"notes"`
}

type DiagnosticsService struct {
	Runtime *Runtime
}

var (
	diagnosticBearerPattern = regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[^\s,;]+`)
	diagnosticSecretPattern = regexp.MustCompile(`(?i)((?:token|secret|password|passphrase|privatekey|private_key|api[_-]?key)\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;]+)`)
	diagnosticQueryPattern  = regexp.MustCompile(`(?i)([?&](?:token|secret|password|passphrase|api[_-]?key)=)[^&\s]+`)
)

func (s *DiagnosticsService) Report() DiagnosticsReport {
	runtimeState := s.runtime()
	server := ServerService{Runtime: runtimeState}
	configService := NewConfigServiceWithRuntime(runtimeState)

	databaseInfo := server.GetDatabaseInfo()
	if databaseInfo == nil {
		databaseInfo = map[string]int64{}
	}
	for key, value := range diagnosticsCountRows() {
		databaseInfo[key] = value
	}

	report := DiagnosticsReport{
		GeneratedAt: time.Now().Unix(),
		System: map[string]interface{}{
			"appVersion": config.GetVersion(),
			"go":         runtime.Version(),
			"sys":        server.GetSystemInfo(),
			"sbd":        server.GetSingboxInfo(),
			"mem":        server.GetMemInfo(),
			"dsk":        server.GetDiskInfo(),
		},
		Database: databaseInfo,
		Settings: diagnosticsSettingsSnapshot(SettingService{}),
		Logs: map[string][]string{
			"recentWarnings": redactDiagnosticLogLines(safeDiagnosticsLogs(&server, "20", "warning", "", "")),
			"recentErrors":   redactDiagnosticLogLines(safeDiagnosticsLogs(&server, "20", "error", "", "")),
			"recentCore":     redactDiagnosticLogLines(safeDiagnosticsLogs(&server, "20", "debug", "core", "")),
		},
		LogInsights: server.GetLogInsights(200),
	}

	report.Checks = append(report.Checks, s.coreCheck(runtimeState))
	report.Checks = append(report.Checks, s.databaseCheck())
	report.Checks = append(report.Checks, s.configChecks(configService, runtimeState)...)
	report.Checks = append(report.Checks, diagnosticsSettingsChecks(SettingService{})...)
	report.Health = summarizeDiagnosticChecks(report.Checks)
	return report
}

func (s *DiagnosticsService) Bundle() DiagnosticsBundle {
	runtimeState := s.runtime()
	server := ServerService{Runtime: runtimeState}
	logs, _ := server.GetLogEntriesFiltered("300", "debug", "", "", "")
	return DiagnosticsBundle{
		GeneratedAt: time.Now().Unix(),
		Report:      s.Report(),
		Logs:        redactDiagnosticLogEntries(logs),
		Notes: []string{
			"Diagnostic bundle is read-only and does not include raw sing-box config, database dump, or private keys. Common secret patterns in included log messages are redacted.",
			"Server-side journalctl/systemd/port checks are available from: sudo solovey-ui doctor --full",
		},
	}
}

func (s *DiagnosticsService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *DiagnosticsService) coreCheck(runtimeState *Runtime) DiagnosticCheck {
	coreInstance := runtimeState.Core()
	if coreInstance == nil {
		return DiagnosticCheck{
			Key:     "core_runtime",
			Title:   "Core runtime",
			Status:  DiagnosticStatusFail,
			Message: "sing-box core runtime is not initialized",
		}
	}
	if !coreInstance.IsRunning() {
		return DiagnosticCheck{
			Key:     "core_running",
			Title:   "Core running",
			Status:  DiagnosticStatusFail,
			Message: "sing-box core is not running",
		}
	}
	return DiagnosticCheck{
		Key:     "core_running",
		Title:   "Core running",
		Status:  DiagnosticStatusOK,
		Message: "sing-box core is running",
	}
}

func (s *DiagnosticsService) databaseCheck() DiagnosticCheck {
	db := database.GetDB()
	if db == nil {
		return DiagnosticCheck{
			Key:     "database",
			Title:   "Database",
			Status:  DiagnosticStatusFail,
			Message: "database is not initialized",
		}
	}

	var quick string
	if err := db.Raw("PRAGMA quick_check").Scan(&quick).Error; err != nil {
		return DiagnosticCheck{
			Key:     "database",
			Title:   "Database",
			Status:  DiagnosticStatusFail,
			Message: err.Error(),
		}
	}
	if quick != "ok" {
		return DiagnosticCheck{
			Key:     "database",
			Title:   "Database",
			Status:  DiagnosticStatusFail,
			Message: quick,
		}
	}
	return DiagnosticCheck{
		Key:     "database",
		Title:   "Database",
		Status:  DiagnosticStatusOK,
		Message: "sqlite quick_check passed",
	}
}

func (s *DiagnosticsService) configChecks(configService *ConfigService, runtimeState *Runtime) []DiagnosticCheck {
	rawConfig, err := configService.GetConfig("")
	if err != nil {
		return []DiagnosticCheck{{
			Key:     "config_build",
			Title:   "Generated config",
			Status:  DiagnosticStatusFail,
			Message: err.Error(),
		}}
	}

	checks := []DiagnosticCheck{{
		Key:     "config_build",
		Title:   "Generated config",
		Status:  DiagnosticStatusOK,
		Message: "configuration was generated",
		Details: diagnosticsConfigDetails(*rawConfig),
	}}
	checks = append(checks, diagnosticsNaiveSupportCheck(*rawConfig)...)

	var parsed option.Options
	ctx := coreContext(runtimeState)
	if err := parsed.UnmarshalJSONContext(ctx, *rawConfig); err != nil {
		checks = append(checks, DiagnosticCheck{
			Key:     "config_parse",
			Title:   "sing-box parse",
			Status:  DiagnosticStatusFail,
			Message: err.Error(),
		})
		return checks
	}
	checks = append(checks, DiagnosticCheck{
		Key:     "config_parse",
		Title:   "sing-box parse",
		Status:  DiagnosticStatusOK,
		Message: "generated config is accepted by the bundled sing-box parser",
	})
	return checks
}

func diagnosticsNaiveSupportCheck(raw []byte) []DiagnosticCheck {
	if core.SupportsNaiveOutbound {
		return nil
	}
	count := diagnosticsCountType(raw, "naive")
	if count == 0 {
		return nil
	}
	return []DiagnosticCheck{{
		Key:     "naive_outbound_build_tag",
		Title:   "Naive outbound",
		Status:  DiagnosticStatusWarn,
		Message: "generated config uses naive, but this binary was built without with_naive_outbound",
		Details: map[string]interface{}{
			"count": count,
			"fix":   "build/release with -tags with_naive_outbound and the required cronet-go toolchain",
		},
	}}
}

func diagnosticsCountType(raw []byte, outboundType string) int {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0
	}
	count := 0
	for _, section := range []string{"inbounds", "outbounds"} {
		var rows []map[string]interface{}
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

func coreContext(runtimeState *Runtime) context.Context {
	if runtimeState != nil {
		if coreInstance := runtimeState.Core(); coreInstance != nil {
			return coreInstance.GetCtx()
		}
	}
	return core.NewCore().GetCtx()
}

func diagnosticsConfigDetails(raw []byte) map[string]interface{} {
	var doc map[string]json.RawMessage
	details := map[string]interface{}{"bytes": len(raw)}
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

func diagnosticsSettingsChecks(settings SettingService) []DiagnosticCheck {
	checks := make([]DiagnosticCheck, 0, 4)
	if port, err := settings.GetPort(); err != nil {
		checks = append(checks, diagnosticSettingFail("web_port", "Panel port", err))
	} else if port <= 0 || port > 65535 {
		checks = append(checks, DiagnosticCheck{Key: "web_port", Title: "Panel port", Status: DiagnosticStatusFail, Message: fmt.Sprintf("invalid port %d", port)})
	} else {
		checks = append(checks, DiagnosticCheck{Key: "web_port", Title: "Panel port", Status: DiagnosticStatusOK, Message: fmt.Sprintf("panel port is %d", port)})
	}

	if port, err := settings.GetSubPort(); err != nil {
		checks = append(checks, diagnosticSettingFail("sub_port", "Subscription port", err))
	} else if port <= 0 || port > 65535 {
		checks = append(checks, DiagnosticCheck{Key: "sub_port", Title: "Subscription port", Status: DiagnosticStatusFail, Message: fmt.Sprintf("invalid port %d", port)})
	} else {
		checks = append(checks, DiagnosticCheck{Key: "sub_port", Title: "Subscription port", Status: DiagnosticStatusOK, Message: fmt.Sprintf("subscription port is %d", port)})
	}

	return checks
}

func diagnosticSettingFail(key string, title string, err error) DiagnosticCheck {
	return DiagnosticCheck{
		Key:     key,
		Title:   title,
		Status:  DiagnosticStatusFail,
		Message: err.Error(),
	}
}

func diagnosticsSettingsSnapshot(settings SettingService) map[string]interface{} {
	snapshot := map[string]interface{}{}
	addStringSetting(snapshot, "webListen", settings.GetListen)
	addIntSetting(snapshot, "webPort", settings.GetPort)
	addStringSetting(snapshot, "webPath", settings.GetWebPath)
	addStringSetting(snapshot, "webDomain", settings.GetWebDomain)
	addStringSetting(snapshot, "webURI", settings.GetWebURI)
	addStringSetting(snapshot, "subListen", settings.GetSubListen)
	addIntSetting(snapshot, "subPort", settings.GetSubPort)
	addStringSetting(snapshot, "subPath", settings.GetSubPath)
	addStringSetting(snapshot, "subDomain", settings.GetSubDomain)
	addStringSetting(snapshot, "subURI", settings.GetSubURI)
	addBoolSetting(snapshot, "subLinkEnable", settings.GetSubLinkEnable)
	addBoolSetting(snapshot, "subJsonEnable", settings.GetSubJsonEnable)
	addBoolSetting(snapshot, "subClashEnable", settings.GetSubClashEnable)
	addBoolSetting(snapshot, "subSecretRequired", settings.GetSubSecretRequired)
	return snapshot
}

func addStringSetting(snapshot map[string]interface{}, key string, getter func() (string, error)) {
	if value, err := getter(); err == nil {
		snapshot[key] = value
	}
}

func addIntSetting(snapshot map[string]interface{}, key string, getter func() (int, error)) {
	if value, err := getter(); err == nil {
		snapshot[key] = value
	}
}

func addBoolSetting(snapshot map[string]interface{}, key string, getter func() (bool, error)) {
	if value, err := getter(); err == nil {
		snapshot[key] = value
	}
}

func safeDiagnosticsLogs(server *ServerService, count string, level string, source string, filter string) []string {
	logs, err := server.GetLogsFiltered(count, level, source, filter)
	if err != nil {
		return nil
	}
	return logs
}

func redactDiagnosticLogLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, redactDiagnosticText(line))
	}
	return out
}

func redactDiagnosticLogEntries(entries []LogEntry) []LogEntry {
	if len(entries) == 0 {
		return entries
	}
	out := make([]LogEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Message = redactDiagnosticText(entry.Message)
		out = append(out, entry)
	}
	return out
}

func redactDiagnosticText(value string) string {
	value = diagnosticBearerPattern.ReplaceAllString(value, `${1}[redacted]`)
	value = diagnosticSecretPattern.ReplaceAllString(value, `${1}[redacted]`)
	return diagnosticQueryPattern.ReplaceAllString(value, `${1}[redacted]`)
}

func summarizeDiagnosticChecks(checks []DiagnosticCheck) DiagnosticsHealth {
	counts := map[string]int{
		string(DiagnosticStatusOK):   0,
		string(DiagnosticStatusWarn): 0,
		string(DiagnosticStatusFail): 0,
	}
	for _, check := range checks {
		counts[string(check.Status)]++
	}
	status := "healthy"
	if counts[string(DiagnosticStatusFail)] > 0 {
		status = "down"
	} else if counts[string(DiagnosticStatusWarn)] > 0 {
		status = "degraded"
	}
	return DiagnosticsHealth{Status: status, Counts: counts}
}

func diagnosticsCountRows() map[string]int64 {
	db := database.GetDB()
	if db == nil {
		return nil
	}
	counts := make(map[string]int64)
	countModel := func(key string, value interface{}) {
		var count int64
		if err := db.Model(value).Count(&count).Error; err == nil {
			counts[key] = count
		}
	}
	countModel("settings", &model.Setting{})
	countModel("tls", &model.Tls{})
	countModel("users", &model.User{})
	countModel("tokens", &model.Tokens{})
	countModel("auditEvents", &model.AuditEvent{})
	return counts
}
