package datalifecycle

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

func (m *Manager) ReadJournal(ctx context.Context, after uint64, limit int) ([]operationcoordination.JournalEvent, error) {
	if m == nil || ctx == nil || m.database() == nil || limit < 1 || limit > operationcoordination.MaxJournalPage+1 {
		return nil, errors.New("data journal request invalid")
	}
	var rows []struct {
		model.DataLifecycleJournal
		Kind    string
		OwnerID string
	}
	err := m.database().WithContext(ctx).Table("data_lifecycle_journal_v1 AS j").
		Select("j.sequence, substr(j.operation_id,1,97) AS operation_id, substr(j.state,1,65) AS state, substr(j.event,1,97) AS event, substr(j.reason_code,1,97) AS reason_code, j.revision, j.created_at, substr(o.kind,1,33) AS kind, substr(o.owner_id,1,97) AS owner_id").
		Joins("JOIN data_lifecycle_operations_v1 AS o ON o.operation_id = j.operation_id").
		Where("j.sequence > ?", after).Order("j.sequence ASC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]operationcoordination.JournalEvent, 0, len(rows))
	for _, row := range rows {
		resource := "database"
		if row.Kind == "DROP_DATA" {
			resource = "durable-owner:unavailable"
			if safeOwnerID(row.OwnerID) {
				resource = "durable-owner:" + row.OwnerID
			}
		}
		result = append(result, operationcoordination.ProjectEvent(operationcoordination.NewReference("data", resource, row.OperationID),
			row.Sequence, row.Revision, row.Event, row.State, row.ReasonCode, row.CreatedAt))
	}
	return result, nil
}
