package telegram

import (
	"github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

func (s *Service) NotifyEvent(event string, fields map[string]string) {
	enabled, err := s.telegramEnabled()
	if err != nil || !enabled || s.Runtime == nil {
		return
	}
	message := "S-UI event: " + redact.String(event)
	for key, value := range fields {
		if value == "" {
			continue
		}
		if redact.IsSensitiveKey(key) {
			value = redact.Marker
		} else {
			value = redact.String(value)
		}
		message += "\n" + key + ": " + value
	}
	if notifier := s.Runtime.Notifier(); notifier != nil {
		notifier.Enqueue(telegram.Notification{Event: event, Text: message})
	}
}
