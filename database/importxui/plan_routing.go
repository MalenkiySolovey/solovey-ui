package importxui

import (
	"context"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui/mapping"
	"github.com/MalenkiySolovey/solovey-ui/database/importxui/source"
)

// mappedHasDNS reports whether the migrated config carries DNS servers or
// rules. A source whose only migratable content is DNS must not be skipped.
func mappedHasDNS(mapped map[string]any) bool {
	dns, ok := mapped["dns"].(map[string]any)
	if !ok {
		return false
	}
	return len(mapping.ToAnySlice(dns["servers"])) > 0 || len(mapping.ToAnySlice(dns["rules"])) > 0
}

// planRoutingDisabledNotice surfaces a warning-only plan item when routing
// import is off but xrayConfig still contains proxy outbounds or WARP endpoints.
func planRoutingDisabledNotice(ctx context.Context, src *source.Database, plan *MigrationPlan) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	xrayConfig, err := src.XrayConfig()
	if err != nil {
		return err
	}
	endpoints, outbounds, _, _ := mapping.MapXrayOutbounds(xrayConfig)
	if len(endpoints) == 0 && len(outbounds) == 0 {
		return nil
	}
	plan.Items = append(plan.Items, warningOnlyItem(
		KindRouting, "xrayConfig", "xrayConfig.outbounds", "config",
		[]string{fmt.Sprintf("%d proxy outbound(s) and %d WARP endpoint(s) in the source are not migrated because routing import is disabled; enable \"Include routing\" to migrate them", len(outbounds), len(endpoints))},
	))
	return nil
}

func planRouting(ctx context.Context, src *source.Database, plan *MigrationPlan) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	xrayConfig, err := src.XrayConfig()
	if err != nil {
		return err
	}
	endpoints, outbounds, targets, outboundWarnings := mapping.MapXrayOutbounds(xrayConfig)
	mapped, warnings, mappedCount, manualCount := mapping.MapXrayRouting(xrayConfig, targets)
	warnings = append(outboundWarnings, warnings...)
	preview, err := marshalJSON(mapped)
	if err != nil {
		return err
	}
	action := ActionCreate
	if xrayConfig == "" || (mappedCount == 0 && manualCount == 0 && len(endpoints) == 0 && len(outbounds) == 0 && !mappedHasDNS(mapped)) {
		action = ActionSkip
	}
	plan.Items = append(plan.Items, PlanItem{
		Kind:        KindRouting,
		SrcID:       "xrayConfig",
		SrcTag:      "xrayConfig.routing",
		DstTag:      "config",
		Action:      action,
		PreviewJSON: preview,
		Warnings:    warnings,
	})
	return nil
}
