//go:build !minimal

package importxui

import (
	"context"

	"github.com/MalenkiySolovey/solovey-component-import-xui/preview"
	"github.com/MalenkiySolovey/solovey-component-import-xui/source"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	tagrefs "github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
)

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
	if item, ok := preview.RoutingDisabledNotice[UnsupportedObject](xrayConfig); ok {
		plan.Items = append(plan.Items, item)
	}
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
	proposal, err := preview.ProposeRouting[UnsupportedObject](xrayConfig)
	if err != nil {
		return err
	}
	endpoints, outbounds := proposal.Endpoints, proposal.Outbounds
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
		refs, err := tagrefs.ProjectedReferences(proposal.Item.PreviewJSON, projected, category, excluded.SrcTag)
		if err != nil {
			return err
		}
		if len(refs) > 0 {
			excluded.Reason = "CAPABILITY_REFERENCE_UNAVAILABLE"
			issues = append(issues, excluded)
		}
	}
	if len(issues) > 0 {
		item := warningOnlyItem(KindRouting, "xrayConfig", "xrayConfig.routing", "config", proposal.Item.Warnings)
		item.Unsupported = issues
		item.Warnings = append(item.Warnings, "Routing import requires unavailable target entities; the complete routing candidate is excluded.")
		plan.Items = append(plan.Items, item)
		return nil
	}
	plan.Items = append(plan.Items, proposal.Item)
	return nil
}
