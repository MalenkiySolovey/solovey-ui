package importxui

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func (s *planningState) planClients(ctx context.Context, tx *gorm.DB, src *source.Database, plan *MigrationPlan, strategy Strategy) error {
	aggs, err := collectClientAggregates(src, s.clientRefs, s.inboundIDBySrc)
	if err != nil {
		return err
	}
	emails := make([]string, 0, len(aggs))
	for email := range aggs {
		emails = append(emails, email)
	}
	sortStrings(emails)
	for _, email := range emails {
		if err := checkContext(ctx); err != nil {
			return err
		}
		client, err := aggs[email].toModel()
		if err != nil {
			return err
		}
		preview, err := marshalJSON(client)
		if err != nil {
			return err
		}
		conflict, err := recordExists(tx, &model.Client{}, "name = ?", email)
		if err != nil {
			return err
		}
		plan.Items = append(plan.Items, PlanItem{
			Kind:        KindClient,
			SrcID:       email,
			SrcTag:      email,
			DstTag:      email,
			Action:      defaultAction(conflict, strategy),
			Conflict:    conflict,
			PreviewJSON: preview,
		})
	}
	return nil
}
