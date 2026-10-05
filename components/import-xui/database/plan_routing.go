//go:build !minimal

package importxui

import (
	"context"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/mapping"
	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/source"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	tagrefs "github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
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

func planRouting(ctx context.Context, src *source.Database, plan *MigrationPlan, snapshot entitycapabilities.Snapshot) error {
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
	issues := routingCapabilityIssues(snapshot, endpoints, outbounds)
	projected := tagrefs.ProjectionRows{Endpoints: endpoints, Outbounds: outbounds}
	for _, excluded := range unsupportedObjects(*plan) {
		if excluded.Kind != KindInbound && excluded.Kind != KindEndpoint {
			continue
		}
		category := "inbounds"
		if excluded.Kind == KindEndpoint {
			category = "endpoints"
		}
		refs, err := tagrefs.ProjectedReferences(preview, projected, category, excluded.SrcTag)
		if err != nil {
			return err
		}
		if len(refs) > 0 {
			excluded.Reason = "CAPABILITY_REFERENCE_UNAVAILABLE"
			issues = append(issues, excluded)
		}
	}
	if len(issues) > 0 {
		item := warningOnlyItem(KindRouting, "xrayConfig", "xrayConfig.routing", "config", warnings)
		item.Unsupported = issues
		item.Warnings = append(item.Warnings, "Routing import requires unavailable target entities; the complete routing candidate is excluded.")
		plan.Items = append(plan.Items, item)
		return nil
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
