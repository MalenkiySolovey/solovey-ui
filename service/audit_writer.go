package service

import (
	"context"
	"time"

	canonical "github.com/MalenkiySolovey/solovey-backend/audit/store"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

const (
	auditQueueCapacity = canonical.DefaultQueueCapacity
	auditBatchSize     = canonical.DefaultBatchSize
	auditFlushInterval = canonical.DefaultFlushInterval
)

type auditWriter = canonical.Writer

func newAuditWriter(capacity, batchSize int, interval time.Duration, write func([]model.AuditEvent) error) *auditWriter {
	writer := canonical.NewWriter(capacity, batchSize, interval, write)
	writer.Warning = logger.Warning
	return writer
}

func StopAuditWriter(ctx context.Context) error { return DefaultRuntime().StopAuditWriter(ctx) }

func (r *Runtime) StopAuditWriter(ctx context.Context) error {
	r = runtimeOrDefault(r)
	writer := r.audit()
	if writer == nil {
		return nil
	}
	err := writer.Stop(ctx)
	r.replaceAuditWriterIfCurrent(writer, writer.DrainPending())
	return err
}
