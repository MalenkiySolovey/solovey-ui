package service

import (
	"context"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type restoreImportPostOpenResult struct {
	settingsInitialized       bool
	encryptedSettingsResealed int
	sessionRotated            bool
}

type restoreImportPostOpenAction struct {
	name string
	run  func(context.Context, *SettingService, *restoreImportPostOpenResult) error
}

func runRestoreImportServicePostOpenActions(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	settingService := &SettingService{}
	result := restoreImportPostOpenResult{}
	if err := runRestoreImportServicePostOpenActionList(ctx, settingService, &result, restoreImportServicePostOpenActions()); err != nil {
		return err
	}
	recordRestoreImportPostOpenAudit(result)
	return nil
}

func restoreImportServicePostOpenActions() []restoreImportPostOpenAction {
	return []restoreImportPostOpenAction{
		{name: "initializing restored settings", run: initializeRestoreImportSettings},
		{name: "resealing restored secret settings", run: resealRestoreImportSecretSettings},
		{name: "rotating restored sessions", run: rotateRestoreImportSessions},
	}
}

func runRestoreImportServicePostOpenActionList(ctx context.Context, settingService *SettingService, result *restoreImportPostOpenResult, actions []restoreImportPostOpenAction) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if settingService == nil {
		settingService = &SettingService{}
	}
	if result == nil {
		result = &restoreImportPostOpenResult{}
	}
	for _, action := range actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if action.run == nil {
			continue
		}
		if err := action.run(ctx, settingService, result); err != nil {
			return common.NewErrorf("Error %s: %v", action.name, err)
		}
	}
	return ctx.Err()
}

func initializeRestoreImportSettings(ctx context.Context, settingService *SettingService, result *restoreImportPostOpenResult) error {
	if _, err := settingService.GetAllSetting(); err != nil {
		return err
	}
	result.settingsInitialized = true
	return ctx.Err()
}

func resealRestoreImportSecretSettings(ctx context.Context, settingService *SettingService, result *restoreImportPostOpenResult) error {
	resealed, err := settingService.ResealSecretSettings()
	if err != nil {
		return err
	}
	result.encryptedSettingsResealed = resealed
	return ctx.Err()
}

func rotateRestoreImportSessions(ctx context.Context, settingService *SettingService, result *restoreImportPostOpenResult) error {
	generation, err := settingService.RotateSessionGeneration()
	if err != nil {
		return err
	}
	result.sessionRotated = generation != ""
	return ctx.Err()
}

func recordRestoreImportPostOpenAudit(result restoreImportPostOpenResult) {
	if dbsqlite.DB() == nil {
		return
	}
	if err := (&AuditService{}).Record(AuditEvent{
		Actor:    "system",
		Event:    "db_restore_post_actions",
		Resource: "database",
		Severity: AuditSeverityInfo,
		Details: map[string]any{
			"encryptedSettingsResealed": result.encryptedSettingsResealed,
			"sessionRotated":            result.sessionRotated,
			"settingsInitialized":       result.settingsInitialized,
		},
	}); err != nil {
		logger.Warning("restore post-open audit failed:", err)
	}
}
