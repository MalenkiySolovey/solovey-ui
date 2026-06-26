package service

import (
	"context"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

func newDefaultTelegramNotifier() *integrationtelegram.Notifier {
	return integrationtelegram.NewNotifier(
		integrationtelegram.QueueCapacity,
		func(text string) integrationtelegram.Result {
			return integrationtelegram.Result((&TelegramService{}).send(text))
		},
		recordTelegramNotifierAudit,
	)
}

func StopTelegramNotifier(ctx context.Context) error {
	runtime := DefaultRuntime()
	notifier := runtime.telegram()
	if notifier == nil {
		return nil
	}

	err := notifier.Stop(ctx)

	runtime.replaceTelegramNotifierIfCurrent(notifier)
	return err
}

func (s *TelegramService) NotifyTelegramEvent(event string, fields map[string]string) {
	s.implementation().NotifyEvent(event, fields)
}

func recordTelegramNotifierAudit(event string, details map[string]any) {
	if dbsqlite.DB() == nil {
		return
	}
	if err := (&AuditService{}).Record(AuditEvent{
		Event:    event,
		Resource: "notifier",
		Severity: AuditSeverityWarn,
		Details:  details,
	}); err != nil {
		logger.Warning("telegram notifier audit failed:", err)
	}
}
