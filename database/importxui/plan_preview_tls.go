package importxui

import (
	"context"

	"gorm.io/gorm"
)

func (s *importState) planTLS(ctx context.Context, tx *gorm.DB, src *sourceDB, plan *MigrationPlan, strategy Strategy) error {
	return src.eachInbound(func(row xuiInboundRow) error {
		if err := checkContext(ctx); err != nil {
			return err
		}
		spec, warnings, err := extractReality(row)
		if err != nil {
			return err
		}
		if spec == nil {
			return s.planPlainTLS(tx, row, plan, strategy)
		}
		if existing, ok := s.realityByKey[spec.Key]; ok {
			s.realityBySource[row.ID] = existing
			return nil
		}
		s.realityByKey[spec.Key] = spec
		s.realityBySource[row.ID] = spec
		record, err := buildTLSRecord(*spec)
		if err != nil {
			return err
		}
		preview, err := marshalJSON(record)
		if err != nil {
			return err
		}
		_, conflict, err := findExistingRealityTLS(tx, *spec)
		if err != nil {
			return err
		}
		plan.Items = append(plan.Items, PlanItem{
			Kind:        KindTLS,
			SrcID:       spec.Key,
			SrcTag:      row.Tag,
			DstTag:      record.Name,
			Action:      defaultAction(conflict, strategy),
			Conflict:    conflict,
			PreviewJSON: preview,
			Warnings:    warnings,
		})
		return nil
	})
}

func (s *importState) planPlainTLS(tx *gorm.DB, row xuiInboundRow, plan *MigrationPlan, strategy Strategy) error {
	spec, warnings, err := extractPlainTLS(row)
	if err != nil {
		return err
	}
	if spec == nil {
		if len(warnings) > 0 {
			plan.Items = append(plan.Items, warningOnlyItem(KindTLS, "tls-warn:"+row.Tag, row.Tag, row.Tag, warnings))
		}
		return nil
	}
	if existing, ok := s.plainTLSByKey[spec.Key]; ok {
		s.plainTLSBySource[row.ID] = existing
		return nil
	}
	s.plainTLSByKey[spec.Key] = spec
	s.plainTLSBySource[row.ID] = spec
	record, err := buildPlainTLSRecord(*spec)
	if err != nil {
		return err
	}
	preview, err := marshalJSON(record)
	if err != nil {
		return err
	}
	_, conflict, err := findExistingPlainTLS(tx, *spec)
	if err != nil {
		return err
	}
	plan.Items = append(plan.Items, PlanItem{
		Kind:        KindTLS,
		SrcID:       spec.Key,
		SrcTag:      row.Tag,
		DstTag:      record.Name,
		Action:      defaultAction(conflict, strategy),
		Conflict:    conflict,
		PreviewJSON: preview,
		Warnings:    warnings,
	})
	return nil
}
