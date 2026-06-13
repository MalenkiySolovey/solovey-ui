package importxui

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func (s *importState) importTLS(tx *gorm.DB, src *sourceDB) error {
	return src.eachInbound(func(row xuiInboundRow) error {
		spec, warnings, err := extractReality(row)
		if err != nil {
			return err
		}
		s.report.warnAll(warnings)
		if spec != nil {
			if existing, ok := s.realityByKey[spec.Key]; ok {
				s.realityBySource[row.ID] = existing
				s.report.Summary.TLS.Reused++
				return nil
			}
			s.realityByKey[spec.Key] = spec
			s.realityBySource[row.ID] = spec
			existing, found, err := findExistingRealityTLS(tx, *spec)
			if err != nil {
				return err
			}
			if found {
				s.tlsIDByKey[spec.Key] = existing.Id
				s.report.Summary.TLS.Reused++
				return nil
			}
			record, err := buildTLSRecord(*spec)
			if err != nil {
				return err
			}
			sortOrder, err := nextImportSortOrder(tx, &model.Tls{})
			if err != nil {
				return err
			}
			record.SortOrder = sortOrder
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			s.tlsIDByKey[spec.Key] = record.Id
			s.report.Summary.TLS.Created++
			return nil
		}
		return s.importPlainTLS(tx, row)
	})
}

// importPlainTLS migrates a non-reality TLS inbound's inline certificate into
// an s-ui TLS record, deduplicating by certificate content.
func (s *importState) importPlainTLS(tx *gorm.DB, row xuiInboundRow) error {
	spec, warnings, err := extractPlainTLS(row)
	if err != nil {
		return err
	}
	s.report.warnAll(warnings)
	if spec == nil {
		return nil
	}
	if existing, ok := s.plainTLSByKey[spec.Key]; ok {
		s.plainTLSBySource[row.ID] = existing
		s.report.Summary.TLS.Reused++
		return nil
	}
	s.plainTLSByKey[spec.Key] = spec
	s.plainTLSBySource[row.ID] = spec
	existing, found, err := findExistingPlainTLS(tx, *spec)
	if err != nil {
		return err
	}
	if found {
		s.tlsIDByKey[spec.Key] = existing.Id
		s.report.Summary.TLS.Reused++
		return nil
	}
	record, err := buildPlainTLSRecord(*spec)
	if err != nil {
		return err
	}
	sortOrder, err := nextImportSortOrder(tx, &model.Tls{})
	if err != nil {
		return err
	}
	record.SortOrder = sortOrder
	if err := tx.Create(&record).Error; err != nil {
		return err
	}
	s.tlsIDByKey[spec.Key] = record.Id
	s.report.Summary.TLS.Created++
	return nil
}
