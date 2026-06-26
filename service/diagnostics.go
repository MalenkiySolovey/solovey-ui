package service

import (
	"fmt"
	"runtime"
	"time"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	opsdiagnostics "github.com/MalenkiySolovey/solovey-ui/internal/ops/diagnostics"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

type DiagnosticsReport struct {
	GeneratedAt int64                      `json:"generatedAt"`
	Health      opsdiagnostics.Health      `json:"health"`
	Checks      []opsdiagnostics.Check     `json:"checks"`
	System      map[string]interface{}     `json:"system"`
	Database    map[string]int64           `json:"database,omitempty"`
	Settings    map[string]interface{}     `json:"settings,omitempty"`
	Logs        map[string][]string        `json:"logs"`
	LogInsights opsdiagnostics.LogInsights `json:"logInsights"`
}

type DiagnosticsBundle struct {
	GeneratedAt int64                     `json:"generatedAt"`
	Report      DiagnosticsReport         `json:"report"`
	Logs        []opsdiagnostics.LogEntry `json:"logs"`
	Notes       []string                  `json:"notes"`
}

type DiagnosticsService struct {
	Runtime *Runtime
}

func (s *DiagnosticsService) Report() DiagnosticsReport {
	runtimeState := s.runtime()
	server := NewServerService(runtimeState)
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
			"appVersion": configidentity.GetVersion(),
			"go":         runtime.Version(),
			"sys":        server.GetSystemInfo(),
			"sbd":        server.GetSingboxInfo(),
			"mem":        server.GetMemInfo(),
			"dsk":        server.GetDiskInfo(),
		},
		Database: databaseInfo,
		Settings: diagnosticsSettingsSnapshot(SettingService{}),
		Logs: map[string][]string{
			"recentWarnings": opsdiagnostics.RedactLogLines(safeDiagnosticsLogs(&server, "20", "warning", "", "")),
			"recentErrors":   opsdiagnostics.RedactLogLines(safeDiagnosticsLogs(&server, "20", "error", "", "")),
			"recentCore":     opsdiagnostics.RedactLogLines(safeDiagnosticsLogs(&server, "20", "debug", "core", "")),
		},
		LogInsights: server.GetLogInsights(200),
	}

	report.Checks = append(report.Checks, s.coreCheck(runtimeState))
	report.Checks = append(report.Checks, s.databaseCheck())
	report.Checks = append(report.Checks, s.configChecks(configService)...)
	report.Checks = append(report.Checks, diagnosticsSettingsChecks(SettingService{})...)
	report.Health = opsdiagnostics.SummarizeChecks(report.Checks)
	return report
}

func (s *DiagnosticsService) Bundle() DiagnosticsBundle {
	runtimeState := s.runtime()
	server := NewServerService(runtimeState)
	logs, _ := server.GetLogEntriesFiltered("300", "debug", "", "", "")
	return DiagnosticsBundle{
		GeneratedAt: time.Now().Unix(),
		Report:      s.Report(),
		Logs:        opsdiagnostics.RedactLogEntries(logs),
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

func (s *DiagnosticsService) coreCheck(runtimeState *Runtime) opsdiagnostics.Check {
	coreInstance := runtimeState.Core()
	if coreInstance == nil {
		return opsdiagnostics.Check{
			Key:     "core_runtime",
			Title:   "Core runtime",
			Status:  opsdiagnostics.StatusFail,
			Message: "sing-box core runtime is not initialized",
		}
	}
	if !coreInstance.IsRunning() {
		return opsdiagnostics.Check{
			Key:     "core_running",
			Title:   "Core running",
			Status:  opsdiagnostics.StatusFail,
			Message: "sing-box core is not running",
		}
	}
	return opsdiagnostics.Check{
		Key:     "core_running",
		Title:   "Core running",
		Status:  opsdiagnostics.StatusOK,
		Message: "sing-box core is running",
	}
}

func (s *DiagnosticsService) databaseCheck() opsdiagnostics.Check {
	db := dbsqlite.DB()
	if db == nil {
		return opsdiagnostics.Check{
			Key:     "database",
			Title:   "Database",
			Status:  opsdiagnostics.StatusFail,
			Message: "database is not initialized",
		}
	}

	var quick string
	if err := db.Raw("PRAGMA quick_check").Scan(&quick).Error; err != nil {
		return opsdiagnostics.Check{
			Key:     "database",
			Title:   "Database",
			Status:  opsdiagnostics.StatusFail,
			Message: err.Error(),
		}
	}
	if quick != "ok" {
		return opsdiagnostics.Check{
			Key:     "database",
			Title:   "Database",
			Status:  opsdiagnostics.StatusFail,
			Message: quick,
		}
	}
	return opsdiagnostics.Check{
		Key:     "database",
		Title:   "Database",
		Status:  opsdiagnostics.StatusOK,
		Message: "sqlite quick_check passed",
	}
}

func (s *DiagnosticsService) configChecks(configService *ConfigService) []opsdiagnostics.Check {
	rawConfig, err := configService.GetConfig("")
	if err != nil {
		return []opsdiagnostics.Check{{
			Key:     "config_build",
			Title:   "Generated config",
			Status:  opsdiagnostics.StatusFail,
			Message: err.Error(),
		}}
	}

	checks := []opsdiagnostics.Check{{
		Key:     "config_build",
		Title:   "Generated config",
		Status:  opsdiagnostics.StatusOK,
		Message: "configuration was generated",
		Details: opsdiagnostics.ConfigDetails(*rawConfig),
	}}
	checks = append(checks, opsdiagnostics.NaiveSupportCheck(*rawConfig, registry.SupportsNaiveOutbound)...)

	if err := singboxvalidation.ValidateConfig(*rawConfig); err != nil {
		checks = append(checks, opsdiagnostics.Check{
			Key:     "config_parse",
			Title:   "sing-box parse",
			Status:  opsdiagnostics.StatusFail,
			Message: err.Error(),
		})
		return checks
	}
	checks = append(checks, opsdiagnostics.Check{
		Key:     "config_parse",
		Title:   "sing-box parse",
		Status:  opsdiagnostics.StatusOK,
		Message: "generated config is accepted by the bundled sing-box dry validator",
	})
	return checks
}

func diagnosticsSettingsChecks(settings SettingService) []opsdiagnostics.Check {
	checks := make([]opsdiagnostics.Check, 0, 4)
	if port, err := settings.GetPort(); err != nil {
		checks = append(checks, diagnosticSettingFail("web_port", "Panel port", err))
	} else if port <= 0 || port > 65535 {
		checks = append(checks, opsdiagnostics.Check{Key: "web_port", Title: "Panel port", Status: opsdiagnostics.StatusFail, Message: fmt.Sprintf("invalid port %d", port)})
	} else {
		checks = append(checks, opsdiagnostics.Check{Key: "web_port", Title: "Panel port", Status: opsdiagnostics.StatusOK, Message: fmt.Sprintf("panel port is %d", port)})
	}

	if port, err := settings.GetSubPort(); err != nil {
		checks = append(checks, diagnosticSettingFail("sub_port", "Subscription port", err))
	} else if port <= 0 || port > 65535 {
		checks = append(checks, opsdiagnostics.Check{Key: "sub_port", Title: "Subscription port", Status: opsdiagnostics.StatusFail, Message: fmt.Sprintf("invalid port %d", port)})
	} else {
		checks = append(checks, opsdiagnostics.Check{Key: "sub_port", Title: "Subscription port", Status: opsdiagnostics.StatusOK, Message: fmt.Sprintf("subscription port is %d", port)})
	}

	return checks
}

func diagnosticSettingFail(key string, title string, err error) opsdiagnostics.Check {
	return opsdiagnostics.Check{
		Key:     key,
		Title:   title,
		Status:  opsdiagnostics.StatusFail,
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

func diagnosticsCountRows() map[string]int64 {
	db := dbsqlite.DB()
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
