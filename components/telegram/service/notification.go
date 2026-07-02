//go:build !minimal

package telegram

import (
	"github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

func (s *Service) NotifyEvent(event string, fields map[string]string) {
	enabled, err := s.telegramEnabled()
	if err != nil || !enabled || s.Notifier == nil {
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
	if s.Notifier != nil {
		s.Notifier.Enqueue(telegram.Notification{Event: event, Text: message})
	}
}
