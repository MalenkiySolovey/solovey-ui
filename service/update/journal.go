package update

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

func (m *LifecycleManager) ReadJournal(ctx context.Context, after uint64, limit int) ([]operationcoordination.JournalEvent, error) {
	if m == nil || ctx == nil || limit < 1 || limit > operationcoordination.MaxJournalPage+1 {
		return nil, errors.New("update journal request invalid")
	}
	db, err := m.repo.db(ctx)
	if err != nil {
		return nil, err
	}
	var rows []model.UpdateJournal
	if err := db.Select("sequence, substr(operation_id,1,97) AS operation_id, substr(state,1,65) AS state, substr(event,1,97) AS event, substr(reason_code,1,97) AS reason_code, revision, created_at").
		Where("sequence > ?", after).Order("sequence ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]operationcoordination.JournalEvent, 0, len(rows))
	for _, row := range rows {
		result = append(result, operationcoordination.ProjectEvent(operationcoordination.NewReference("update", "panel", row.OperationID),
			row.Sequence, row.Revision, row.Event, row.State, row.ReasonCode, row.CreatedAt))
	}
	return result, nil
}
