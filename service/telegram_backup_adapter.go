package service

import (
	"context"

	telegramimpl "github.com/MalenkiySolovey/solovey-ui/service/telegram"
)

const (
	TelegramBackupTriggerManual    = telegramimpl.TelegramBackupTriggerManual
	TelegramBackupTriggerScheduled = telegramimpl.TelegramBackupTriggerScheduled
)

type TelegramBackupResult telegramimpl.TelegramBackupResult

type TelegramBackupService struct {
	SettingService
	TelegramService
	AuditService
	SendDocument func(filename string, payload []byte, caption string) TelegramResult
}

func ContextWithTelegramBackupActor(ctx context.Context, actor string) context.Context {
	return telegramimpl.ContextWithTelegramBackupActor(ctx, actor)
}

func (s *TelegramBackupService) RunOnce(ctx context.Context, trigger string) TelegramBackupResult {
	telegramService := s.TelegramService
	telegramService.SettingService = s.SettingService
	sendDocument := s.SendDocument
	if sendDocument == nil {
		sendDocument = telegramService.SendTelegramDocument
	}
	implementation := &telegramimpl.TelegramBackupService{
		Settings: &s.SettingService,
		Telegram: telegramService.implementation(),
		SendDocument: func(filename string, payload []byte, caption string) telegramimpl.Result {
			return telegramimpl.Result(sendDocument(filename, payload, caption))
		},
		Audit: func(record telegramimpl.AuditRecord) error {
			return s.AuditService.Record(AuditEvent{
				Actor:    record.Actor,
				Event:    record.Event,
				Resource: record.Resource,
				Severity: record.Severity,
				Details:  record.Details,
			})
		},
	}
	return TelegramBackupResult(implementation.RunOnce(ctx, trigger))
}
