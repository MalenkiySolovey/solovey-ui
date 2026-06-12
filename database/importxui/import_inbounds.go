package importxui

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func (s *importState) importInboundsAndEndpoints(tx *gorm.DB, src *sourceDB, strategy Strategy) error {
	return src.eachInbound(func(row xuiInboundRow) error {
		if row.Protocol == "wireguard" {
			endpoint, warnings, err := mapWireguardEndpoint(row)
			if err != nil {
				return err
			}
			s.report.warnAll(warnings)
			if endpoint == nil {
				s.report.Summary.Endpoints.Skipped++
				return nil
			}
			imported, err := applyEndpoint(tx, endpoint, strategy, s.report)
			if err != nil {
				return err
			}
			if imported {
				s.report.Summary.Endpoints.Imported++
			}
			return nil
		}

		var tlsID uint
		var reality *realitySpec
		if spec, ok := s.realityBySource[row.ID]; ok {
			reality = spec
			tlsID = s.tlsIDByKey[spec.Key]
		} else if spec, ok := s.plainTLSBySource[row.ID]; ok {
			tlsID = s.tlsIDByKey[spec.Key]
		}
		mapped, err := mapInbound(row, tlsID, reality, s.server)
		if err != nil {
			return err
		}
		s.report.warnAll(mapped.Warnings)
		if mapped.Inbound.Type == "" {
			s.report.Summary.Inbounds.Skipped++
			return nil
		}
		dstID, imported, skipped, err := applyInbound(tx, &mapped.Inbound, strategy, s.report)
		if err != nil {
			return err
		}
		if skipped {
			s.report.Summary.Inbounds.Skipped++
			return nil
		}
		if imported {
			s.report.Summary.Inbounds.Imported++
		}
		s.inboundIDBySrc[row.ID] = dstID
		for i := range mapped.ClientRefs {
			mapped.ClientRefs[i].DstInboundID = dstID
		}
		s.clientRefs = append(s.clientRefs, mapped.ClientRefs...)
		s.report.ByInbound = append(s.report.ByInbound, InboundStat{
			SrcTag:  row.Tag,
			DstTag:  mapped.Inbound.Tag,
			Clients: len(mapped.ClientRefs),
		})
		return nil
	})
}

func applyInbound(tx *gorm.DB, inbound *model.Inbound, strategy Strategy, report *Report) (uint, bool, bool, error) {
	var existing model.Inbound
	err := tx.Where("tag = ?", inbound.Tag).First(&existing).Error
	if err != nil && !database.IsNotFound(err) {
		return 0, false, false, err
	}
	if database.IsNotFound(err) {
		if err := tx.Create(inbound).Error; err != nil {
			return 0, false, false, err
		}
		return inbound.Id, true, false, nil
	}
	report.Summary.Inbounds.Conflicts++
	switch strategy {
	case StrategySkip:
		report.warn(fmt.Sprintf("inbound %s: existing tag skipped by strategy", inbound.Tag))
		return existing.Id, false, true, nil
	case StrategyReplace:
		if err := tx.Delete(&existing).Error; err != nil {
			return 0, false, false, err
		}
		inbound.Id = 0
		if err := tx.Create(inbound).Error; err != nil {
			return 0, false, false, err
		}
		return inbound.Id, true, false, nil
	default:
		inbound.Id = existing.Id
		if err := tx.Save(inbound).Error; err != nil {
			return 0, false, false, err
		}
		return inbound.Id, true, false, nil
	}
}

func applyEndpoint(tx *gorm.DB, endpoint *model.Endpoint, strategy Strategy, report *Report) (bool, error) {
	var existing model.Endpoint
	err := tx.Where("tag = ?", endpoint.Tag).First(&existing).Error
	if err != nil && !database.IsNotFound(err) {
		return false, err
	}
	if database.IsNotFound(err) {
		return true, tx.Create(endpoint).Error
	}
	switch strategy {
	case StrategySkip:
		report.warn(fmt.Sprintf("endpoint %s: existing tag skipped by strategy", endpoint.Tag))
		return false, nil
	case StrategyReplace:
		if err := tx.Delete(&existing).Error; err != nil {
			return false, err
		}
		endpoint.Id = 0
		return true, tx.Create(endpoint).Error
	default:
		endpoint.Id = existing.Id
		return true, tx.Save(endpoint).Error
	}
}
