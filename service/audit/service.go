// Package audit composes Product logging for canonical Backend audit operations.
package audit

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/audit/store"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
	"time"
)

const (
	AuditSeverityInfo              = canonical.AuditSeverityInfo
	AuditSeverityWarn              = canonical.AuditSeverityWarn
	AuditActorMaxBytes             = canonical.AuditActorMaxBytes
	AuditEventMaxBytes             = canonical.AuditEventMaxBytes
	AuditResourceMaxBytes          = canonical.AuditResourceMaxBytes
	AuditSeverityMaxBytes          = canonical.AuditSeverityMaxBytes
	AuditIPMaxBytes                = canonical.AuditIPMaxBytes
	AuditUserAgentMaxBytes         = canonical.AuditUserAgentMaxBytes
	AuditDetailsMaxBytes           = canonical.AuditDetailsMaxBytes
	AuditWriteTimeout              = canonical.AuditWriteTimeout
	DefaultDenialAggregationWindow = canonical.DefaultDenialAggregationWindow
	DefaultDenialAggregationKeys   = canonical.DefaultDenialAggregationKeys
)

type Service = canonical.Service
type Event = canonical.Event
type History = canonical.History
type DenialAggregator = canonical.DenialAggregator

func New(enqueue func(model.AuditEvent), aggregators ...*DenialAggregator) Service {
	service := canonical.New(enqueue, aggregators...)
	service.Warning = logger.Warning
	return service
}
func NewDenialAggregator(window time.Duration, maxKeys int) *DenialAggregator {
	return canonical.NewDenialAggregator(window, maxKeys)
}
func HighFrequencyDenial(event string) bool             { return canonical.HighFrequencyDenial(event) }
func BuildRecord(event Event) (model.AuditEvent, error) { return canonical.BuildRecord(event) }
func WriteEvents(events []model.AuditEvent) error       { return canonical.WriteEvents(events) }
func WriteEventsContext(ctx context.Context, events []model.AuditEvent) error {
	return canonical.WriteEventsContext(ctx, events)
}
func RecordTransaction(tx *gorm.DB, event Event, at int64) error {
	return canonical.RecordTransaction(tx, event, at)
}
