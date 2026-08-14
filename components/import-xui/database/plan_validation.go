//go:build !minimal

package importxui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"gorm.io/gorm"
)

var ErrPlanInvalid = errors.New("plan_invalid")

// validateSubmittedPlan treats the browser-supplied plan as a choice overlay,
// never as migration authority. Source identities, previews, warnings and the
// set of rows are rebuilt from the uploaded database and current destination;
// the caller may only choose a valid action and, for explicitly renameable
// entity kinds, a valid destination identity.
func validateSubmittedPlan(ctx context.Context, db *gorm.DB, srcPath string, submitted MigrationPlan) (MigrationPlan, error) {
	if db == nil || strings.TrimSpace(submitted.Source.Hash) == "" {
		return MigrationPlan{}, ErrPlanInvalid
	}
	strategy := Strategy(submitted.Defaults.Strategy)
	if err := strategy.Validate(); err != nil {
		return MigrationPlan{}, fmt.Errorf("%w: %v", ErrPlanInvalid, err)
	}
	adminMode := AdminMode(submitted.Defaults.AdminMode)
	if err := adminMode.Validate(); err != nil {
		return MigrationPlan{}, fmt.Errorf("%w: %v", ErrPlanInvalid, err)
	}
	canonical, err := Plan(srcPath, PlanOptions{
		Context:         ctx,
		Strategy:        strategy,
		IncludeSettings: submitted.Defaults.IncludeSettings,
		AdminMode:       adminMode,
		OnlyNew:         submitted.Defaults.OnlyNew,
		IncludeHistory:  submitted.Defaults.IncludeHistory,
		IncludeRouting:  submitted.Defaults.IncludeRouting,
	})
	if err != nil {
		return MigrationPlan{}, err
	}
	if submitted.Source.Hash != canonical.Source.Hash {
		return MigrationPlan{}, ErrPlanStale
	}
	if len(submitted.Items) != len(canonical.Items) {
		return MigrationPlan{}, ErrPlanInvalid
	}

	choices := make(map[string]PlanItem, len(submitted.Items))
	for _, item := range submitted.Items {
		key := planKey(item.Kind, item.SrcID)
		if _, duplicate := choices[key]; duplicate {
			return MigrationPlan{}, ErrPlanInvalid
		}
		choices[key] = item
	}

	destinations := make(map[string]string)
	for index := range canonical.Items {
		base := canonical.Items[index]
		key := planKey(base.Kind, base.SrcID)
		choice, ok := choices[key]
		if !ok {
			return MigrationPlan{}, ErrPlanInvalid
		}
		target, err := validatePlanDestination(base, choice.DstTag)
		if err != nil {
			return MigrationPlan{}, err
		}
		conflict, err := planDestinationConflict(db, base, target)
		if err != nil {
			return MigrationPlan{}, err
		}
		if err := validatePlanAction(base, choice.Action, conflict); err != nil {
			return MigrationPlan{}, err
		}
		if choice.Action != ActionSkip && renameablePlanKind(base.Kind) {
			namespace := destinationNamespace(base.Kind) + ":" + target
			if previous, duplicate := destinations[namespace]; duplicate && previous != key {
				return MigrationPlan{}, ErrPlanInvalid
			}
			destinations[namespace] = key
		}
		base.DstTag = target
		base.Action = choice.Action
		base.Conflict = conflict
		canonical.Items[index] = base
		delete(choices, key)
	}
	if len(choices) != 0 {
		return MigrationPlan{}, ErrPlanInvalid
	}
	return *canonical, nil
}

func validatePlanDestination(base PlanItem, submitted string) (string, error) {
	target := strings.TrimSpace(submitted)
	if target != submitted {
		return "", ErrPlanInvalid
	}
	if !renameablePlanKind(base.Kind) {
		if target != base.DstTag {
			return "", ErrPlanInvalid
		}
		return target, nil
	}
	var err error
	switch base.Kind {
	case KindInbound, KindEndpoint:
		err = identity.ValidateTag(target)
	case KindTLS, KindClient, KindAdmin:
		err = identity.ValidateName(target)
	default:
		err = ErrPlanInvalid
	}
	if err != nil {
		return "", fmt.Errorf("%w: invalid destination", ErrPlanInvalid)
	}
	return target, nil
}

func renameablePlanKind(kind string) bool {
	switch kind {
	case KindTLS, KindInbound, KindEndpoint, KindClient, KindAdmin:
		return true
	default:
		return false
	}
}

func destinationNamespace(kind string) string {
	if kind == KindEndpoint {
		return "outbound"
	}
	return kind
}

func validatePlanAction(base PlanItem, action string, conflict bool) error {
	switch action {
	case ActionCreate, ActionMerge, ActionReplace, ActionSkip:
	default:
		return ErrPlanInvalid
	}
	if base.Action == ActionSkip && bytes.Equal(bytes.TrimSpace(base.PreviewJSON), []byte("null")) {
		if action == ActionSkip {
			return nil
		}
		return ErrPlanInvalid
	}
	if base.Kind == KindHistory || base.Kind == KindRouting {
		if action == ActionCreate || action == ActionSkip {
			return nil
		}
		return ErrPlanInvalid
	}
	if conflict {
		if action == ActionMerge || action == ActionReplace || action == ActionSkip {
			return nil
		}
		return ErrPlanStale
	}
	if action == ActionCreate || action == ActionSkip {
		return nil
	}
	return ErrPlanStale
}

func planDestinationConflict(db *gorm.DB, base PlanItem, target string) (bool, error) {
	if target == base.DstTag {
		return base.Conflict, nil
	}
	var count int64
	var query *gorm.DB
	switch base.Kind {
	case KindTLS:
		query = db.Model(&model.Tls{}).Where("name = ?", target)
	case KindInbound:
		query = db.Model(&model.Inbound{}).Where("tag = ?", target)
	case KindEndpoint:
		if err := db.Model(&model.Endpoint{}).Where("tag = ?", target).Count(&count).Error; err != nil {
			return false, err
		}
		var outboundCount int64
		if err := db.Model(&model.Outbound{}).Where("tag = ?", target).Count(&outboundCount).Error; err != nil {
			return false, err
		}
		return count+outboundCount != 0, nil
	case KindClient:
		query = db.Model(&model.Client{}).Where("name = ?", target)
	case KindAdmin:
		query = db.Model(&model.User{}).Where("username = ?", target)
	default:
		return base.Conflict, nil
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count != 0, nil
}
