package importxui

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func (s *importState) planInboundsEndpoints(ctx context.Context, tx *gorm.DB, src *sourceDB, plan *MigrationPlan, strategy Strategy) error {
	return src.eachInbound(func(row xuiInboundRow) error {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if row.Protocol == "wireguard" {
			endpoint, warnings, err := mapWireguardEndpoint(row)
			if err != nil || endpoint == nil {
				if endpoint == nil {
					plan.Items = append(plan.Items, warningOnlyItem(KindEndpoint, row.ID, row.Tag, row.Tag, warnings))
				}
				return err
			}
			preview, err := marshalJSON(endpoint)
			if err != nil {
				return err
			}
			conflict, err := recordExists(tx, &model.Endpoint{}, "tag = ?", endpoint.Tag)
			if err != nil {
				return err
			}
			plan.Items = append(plan.Items, PlanItem{
				Kind:        KindEndpoint,
				SrcID:       row.ID,
				SrcTag:      row.Tag,
				DstTag:      endpoint.Tag,
				Action:      defaultAction(conflict, strategy),
				Conflict:    conflict,
				PreviewJSON: preview,
				Warnings:    warnings,
			})
			return nil
		}
		var reality *realitySpec
		if spec, ok := s.realityBySource[row.ID]; ok {
			reality = spec
		}
		mapped, err := mapInbound(row, 0, reality, s.server)
		if err != nil {
			return err
		}
		if mapped.Inbound.Type == "" {
			plan.Items = append(plan.Items, warningOnlyItem(KindInbound, row.ID, row.Tag, row.Tag, mapped.Warnings))
			return nil
		}
		preview, err := mapped.Inbound.MarshalFull()
		if err != nil {
			return err
		}
		previewJSON, err := marshalJSON(preview)
		if err != nil {
			return err
		}
		conflict, err := recordExists(tx, &model.Inbound{}, "tag = ?", mapped.Inbound.Tag)
		if err != nil {
			return err
		}
		// #nosec G115 -- source x-ui inbound id is a positive SQLite rowid within uint range.
		dstInboundID := uint(row.ID)
		s.inboundIDBySrc[row.ID] = dstInboundID
		for i := range mapped.ClientRefs {
			mapped.ClientRefs[i].DstInboundID = dstInboundID
		}
		s.clientRefs = append(s.clientRefs, mapped.ClientRefs...)
		plan.Items = append(plan.Items, PlanItem{
			Kind:        KindInbound,
			SrcID:       row.ID,
			SrcTag:      row.Tag,
			DstTag:      mapped.Inbound.Tag,
			Action:      defaultAction(conflict, strategy),
			Conflict:    conflict,
			PreviewJSON: previewJSON,
			Warnings:    mapped.Warnings,
		})
		return nil
	})
}
