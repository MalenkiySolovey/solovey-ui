//go:build !minimal

package importxui

import (
	"context"
	"errors"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestApplyRejectsIncompleteDuplicateAndUnsafePlanChoices(t *testing.T) {
	initPlanExtraMainDB(t)
	src := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
	base, err := Plan(src, PlanOptions{Strategy: StrategyMerge})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Items) < 1 {
		t.Fatal("expected at least one planned item")
	}

	tests := []struct {
		name   string
		mutate func(*MigrationPlan)
	}{
		{
			name: "missing item",
			mutate: func(plan *MigrationPlan) {
				plan.Items = plan.Items[1:]
			},
		},
		{
			name: "duplicate item",
			mutate: func(plan *MigrationPlan) {
				plan.Items = append(plan.Items, plan.Items[0])
			},
		},
		{
			name: "unsafe destination",
			mutate: func(plan *MigrationPlan) {
				plan.Items[0].DstTag = "bad\nname"
			},
		},
		{
			name: "invalid action",
			mutate: func(plan *MigrationPlan) {
				plan.Items[0].Action = "delete"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := cloneMigrationPlan(*base)
			tt.mutate(&plan)
			before := destinationRowCount(t, "inbounds")
			if _, err := Apply(src, plan, ApplyOptions{DryRun: true}); !errors.Is(err, ErrPlanInvalid) {
				t.Fatalf("expected ErrPlanInvalid, got %v", err)
			}
			if after := destinationRowCount(t, "inbounds"); after != before {
				t.Fatalf("invalid plan mutated destination: before=%d after=%d", before, after)
			}
		})
	}
}

func TestApplyRebuildsImmutablePlanFields(t *testing.T) {
	initPlanExtraMainDB(t)
	src := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
	plan, err := Plan(src, PlanOptions{Strategy: StrategyMerge})
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.Items {
		plan.Items[i].SrcTag = "forged-source-label"
		plan.Items[i].Conflict = !plan.Items[i].Conflict
		plan.Items[i].PreviewJSON = []byte(`{"forged":true}`)
		plan.Items[i].Warnings = []string{"forged warning"}
	}
	validated, err := validateSubmittedPlan(context.Background(), dbsqlite.DB(), src, *plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range validated.Items {
		if item.SrcTag == "forged-source-label" || string(item.PreviewJSON) == `{"forged":true}` {
			t.Fatalf("client-controlled immutable fields survived validation: %#v", item)
		}
	}
}

func TestApplyRejectsStaleDestinationAndAcceptsFreshReplan(t *testing.T) {
	initPlanExtraMainDB(t)
	src := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
	plan, err := Plan(src, PlanOptions{Strategy: StrategyMerge})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(src, *plan, ApplyOptions{SkipBackup: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(src, *plan, ApplyOptions{DryRun: true}); !errors.Is(err, ErrPlanStale) {
		t.Fatalf("expected stale plan after destination changed, got %v", err)
	}
	fresh, err := Plan(src, PlanOptions{Strategy: StrategyMerge})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(src, *fresh, ApplyOptions{DryRun: true}); err != nil {
		t.Fatalf("fresh plan should be accepted: %v", err)
	}
}

func validPlanExtraInbound() planExtraInbound {
	return planExtraInbound{
		id:       1,
		port:     24443,
		protocol: "vless",
		tag:      "test-vless",
		settings: `{"clients":[{"id":"00000000-0000-4000-8000-000000000001","email":"alice"}]}`,
	}
}

func cloneMigrationPlan(plan MigrationPlan) MigrationPlan {
	plan.Items = append([]PlanItem(nil), plan.Items...)
	return plan
}

func destinationRowCount(t *testing.T, table string) int64 {
	t.Helper()
	var count int64
	if err := dbsqlite.DB().Table(table).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}
