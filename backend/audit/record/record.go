// Package auditrecord is the common bounded, redacted audit record builder.
// It has no process-global database, queue, history or retention authority.
package record

import (
	"encoding/json"
	"errors"

	backendaudit "github.com/MalenkiySolovey/solovey-ui/backend/audit"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-host/redact"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/gorm"
)

const (
	SeverityInfo      = backendaudit.SeverityInfo
	SeverityWarn      = backendaudit.SeverityWarn
	ActorMaxBytes     = backendaudit.ActorMaxBytes
	EventMaxBytes     = backendaudit.EventMaxBytes
	ResourceMaxBytes  = backendaudit.ResourceMaxBytes
	SeverityMaxBytes  = backendaudit.SeverityMaxBytes
	IPMaxBytes        = backendaudit.IPMaxBytes
	UserAgentMaxBytes = backendaudit.UserAgentMaxBytes
	DetailsMaxBytes   = backendaudit.DetailsMaxBytes
)

type Event = backendaudit.Event

func Build(event Event) (model.AuditEvent, error) {
	if event.Severity == "" {
		event.Severity = SeverityInfo
	}
	details, err := json.Marshal(redact.Value(event.Details))
	if err != nil {
		return model.AuditEvent{}, err
	}
	if len(details) > DetailsMaxBytes {
		details, err = json.Marshal(map[string]any{"truncated": true, "originalBytes": len(details)})
		if err != nil {
			return model.AuditEvent{}, err
		}
	}
	return model.AuditEvent{DateTime: time.Now().Unix(), Actor: bounded(event.Actor, ActorMaxBytes),
		Event: bounded(event.Event, EventMaxBytes), Resource: bounded(event.Resource, ResourceMaxBytes),
		Severity: bounded(event.Severity, SeverityMaxBytes), IP: bounded(event.IP, IPMaxBytes),
		UserAgent: bounded(event.UserAgent, UserAgentMaxBytes), Details: details}, nil
}

func bounded(value string, maxBytes int) string {
	value = redact.String(strings.TrimSpace(value))
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

// RecordTransaction keeps an owner's outcome and admission record on the
// supplied transaction, using the same builder as the ordinary audit service.
func RecordTransaction(tx *gorm.DB, event Event, at int64) error {
	if tx == nil || at <= 0 {
		return errors.New("audit transaction is unavailable")
	}
	record, err := Build(event)
	if err != nil {
		return err
	}
	record.DateTime = at
	return tx.Create(&record).Error
}
