package importxui

import (
	"context"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/importxui/mapping"
	"github.com/MalenkiySolovey/solovey-ui/database/importxui/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"

	"gorm.io/gorm"
)

// createNewEndpoints persists WARP/wireguard-outbound endpoints without
// overwriting existing operator-tuned rows.
func createNewEndpoints(tx *gorm.DB, endpoints []model.Endpoint, report *Report) error {
	for i := range endpoints {
		ep := &endpoints[i]
		var existing model.Endpoint
		err := tx.Where("tag = ?", ep.Tag).First(&existing).Error
		if err != nil && !dbsqlite.IsNotFound(err) {
			return err
		}
		if err == nil {
			report.Summary.Endpoints.Skipped++
			report.warn(fmt.Sprintf("endpoint %q already exists; WARP outbound left unchanged", ep.Tag))
			continue
		}
		sortOrder, err := nextImportSortOrder(tx, &model.Endpoint{})
		if err != nil {
			return err
		}
		ep.SortOrder = sortOrder
		if err := tx.Create(ep).Error; err != nil {
			return err
		}
		report.Summary.Endpoints.Imported++
		report.warn(fmt.Sprintf("imported WARP endpoint %q from xray wireguard outbound", ep.Tag))
	}
	return nil
}

// createNewOutbounds persists mapped proxy outbounds without overwriting
// existing operator-tuned rows.
func createNewOutbounds(tx *gorm.DB, outbounds []model.Outbound, report *Report) error {
	for i := range outbounds {
		ob := &outbounds[i]
		var existing model.Outbound
		err := tx.Where("tag = ?", ob.Tag).First(&existing).Error
		if err != nil && !dbsqlite.IsNotFound(err) {
			return err
		}
		if err == nil {
			report.Summary.Outbounds.Skipped++
			report.warn(fmt.Sprintf("outbound %q already exists; left unchanged", ob.Tag))
			continue
		}
		sortOrder, err := nextImportSortOrder(tx, &model.Outbound{})
		if err != nil {
			return err
		}
		ob.SortOrder = sortOrder
		if err := tx.Create(ob).Error; err != nil {
			return err
		}
		report.Summary.Outbounds.Imported++
		report.warn(fmt.Sprintf("imported %s outbound %q from xray outbound", ob.Type, ob.Tag))
	}
	return nil
}

func ensureDirectOutbound(tx *gorm.DB, outbounds []model.Outbound, mapped map[string]any) []model.Outbound {
	if !routingReferencesDirect(mapped) {
		return outbounds
	}
	for i := range outbounds {
		if outbounds[i].Tag == mapping.DirectOutboundTag {
			return outbounds
		}
	}
	var existing model.Outbound
	if err := tx.Where("tag = ?", mapping.DirectOutboundTag).First(&existing).Error; err == nil {
		return outbounds
	}
	return append(outbounds, model.Outbound{Type: mapping.DirectOutboundTag, Tag: mapping.DirectOutboundTag})
}

func routingReferencesDirect(mapped map[string]any) bool {
	route, ok := mapped["route"].(map[string]any)
	if !ok {
		return false
	}
	for _, r := range mapping.ToAnySlice(route["rules"]) {
		if m, ok := r.(map[string]any); ok {
			if ob, _ := m["outbound"].(string); ob == mapping.DirectOutboundTag {
				return true
			}
		}
	}
	for _, rs := range mapping.ToAnySlice(route["rule_set"]) {
		if m, ok := rs.(map[string]any); ok {
			if d, _ := m["download_detour"].(string); d == mapping.DirectOutboundTag {
				return true
			}
		}
	}
	return false
}

func (s *applyState) applyRouting(ctx context.Context, tx *gorm.DB, src *source.Database, _ ApplyOptions) error {
	if !s.hasKind(KindRouting) {
		return nil
	}
	item := s.item(KindRouting, "xrayConfig")
	if item.Action == ActionSkip {
		return nil
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	xrayConfig, err := src.XrayConfig()
	if err != nil {
		return err
	}
	endpoints, outbounds, targets, outboundWarnings := mapping.MapXrayOutbounds(xrayConfig)
	s.report.warnAll(outboundWarnings)
	mapped, warnings, mappedCount, manualCount := mapping.MapXrayRouting(xrayConfig, targets)
	outbounds = ensureDirectOutbound(tx, outbounds, mapped)
	if err := createNewEndpoints(tx, endpoints, s.report); err != nil {
		return err
	}
	for i := range endpoints {
		s.progress("endpoints", endpoints[i].Tag)
	}
	if err := createNewOutbounds(tx, outbounds, s.report); err != nil {
		return err
	}
	for i := range outbounds {
		s.progress("outbounds", outbounds[i].Tag)
	}
	if err := mergeRoutingIntoConfig(tx, mapped); err != nil {
		return err
	}
	s.report.Summary.Routing.Total = mappedCount + manualCount
	s.report.Summary.Routing.Imported = mappedCount
	s.report.Summary.Routing.Skipped = manualCount
	s.report.warnAll(warnings)
	s.progress("routing", "config")
	return nil
}

func mergeRoutingIntoConfig(tx *gorm.DB, mapped map[string]any) error {
	var current string
	if err := tx.Model(model.Setting{}).Select("value").Where("key = ?", "config").Scan(&current).Error; err != nil {
		return err
	}
	merged, changed, err := singboxconfig.MergeMappedRouting(current, mapped)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return upsertSetting(tx, "config", merged)
}
