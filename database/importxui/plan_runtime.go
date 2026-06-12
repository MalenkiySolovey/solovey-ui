package importxui

import (
	"context"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func markOnlyNew(plan *MigrationPlan) {
	for i := range plan.Items {
		if plan.Items[i].Conflict {
			plan.Items[i].Action = ActionSkip
		}
	}
}

func normalizePlan(plan MigrationPlan) map[string]PlanItem {
	items := map[string]PlanItem{}
	for _, item := range plan.Items {
		if item.Action == "" {
			item.Action = ActionCreate
		}
		if item.Kind == KindAdmin && item.AdminMode == "" {
			item.AdminMode = plan.Defaults.AdminMode
		}
		items[planKey(item.Kind, item.SrcID)] = item
	}
	return items
}

func countRunnableItems(plan MigrationPlan) int {
	total := 0
	for _, item := range plan.Items {
		if item.Action != ActionSkip {
			total++
		}
	}
	if total == 0 {
		return 1
	}
	return total
}

func (s *applyState) item(kind string, srcID any) PlanItem {
	if item, ok := s.plan[planKey(kind, srcID)]; ok {
		return item
	}
	return PlanItem{Kind: kind, SrcID: srcID, Action: ActionCreate}
}

func (s *applyState) hasKind(kind string) bool {
	prefix := kind + ":"
	for key := range s.plan {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func planKey(kind string, srcID any) string {
	return kind + ":" + fmt.Sprint(srcID)
}

func (s *applyState) progress(step string, name string) {
	if s.onProgress == nil {
		return
	}
	s.current++
	percent := 0
	if s.total > 0 {
		percent = s.current * 100 / s.total
		if percent > 100 {
			percent = 100
		}
	}
	event := Progress{
		Step:    step,
		Current: s.current,
		Total:   s.total,
		Percent: percent,
	}
	switch step {
	case "clients", "admins":
		event.CurrentName = name
	default:
		event.CurrentTag = name
	}
	s.onProgress(event)
}

func actionToStrategy(action string) Strategy {
	switch action {
	case ActionReplace:
		return StrategyReplace
	case ActionSkip:
		return StrategySkip
	default:
		return StrategyMerge
	}
}

func applyInboundAction(tx *gorm.DB, inbound *model.Inbound, action string, report *Report) (uint, bool, bool, error) {
	return applyInbound(tx, inbound, actionToStrategy(action), report)
}

func applyEndpointAction(tx *gorm.DB, endpoint *model.Endpoint, action string, report *Report) (bool, error) {
	return applyEndpoint(tx, endpoint, actionToStrategy(action), report)
}

func applyClientAction(tx *gorm.DB, agg *clientAggregate, action string, report *Report, hostname string) error {
	return applyClient(tx, agg, actionToStrategy(action), report, hostname)
}

func renameAggregate(agg *clientAggregate, name string) {
	agg.Email = name
	for _, config := range agg.Config {
		if _, ok := config["name"]; ok {
			config["name"] = name
		}
		if _, ok := config["username"]; ok {
			config["username"] = name
		}
	}
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
