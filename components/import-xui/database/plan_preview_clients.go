//go:build !minimal

package importxui

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/source"
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
		preview, err := marshalJSON(map[string]any{
			"name":          email,
			"enabled":       aggs[email].Enable,
			"inbound_count": len(aggs[email].Inbounds),
			"group":         aggs[email].Group,
		})
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
