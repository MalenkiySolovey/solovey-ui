package importxui

import (
	"fmt"
	"sort"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func (s *importState) importClients(tx *gorm.DB, src *sourceDB, strategy Strategy) error {
	aggs, err := collectClientAggregates(src, s.clientRefs, s.inboundIDBySrc)
	if err != nil {
		return err
	}
	s.report.Summary.Clients.UniqueEmails = len(aggs)
	emails := make([]string, 0, len(aggs))
	for email := range aggs {
		emails = append(emails, email)
	}
	sortStrings(emails)
	for _, email := range emails {
		if err := applyClient(tx, aggs[email], strategy, s.report, s.hostname); err != nil {
			return err
		}
	}
	return nil
}

func applyClient(tx *gorm.DB, agg *clientAggregate, strategy Strategy, report *Report, hostname string) error {
	next, err := agg.toModel()
	if err != nil {
		return err
	}
	var existing model.Client
	err = tx.Where("name = ?", agg.Email).First(&existing).Error
	if err != nil && !database.IsNotFound(err) {
		return err
	}
	if database.IsNotFound(err) {
		if next.Links, err = buildClientLinks(tx, next.Config, next.Inbounds, hostname); err != nil {
			return err
		}
		sortOrder, err := nextImportSortOrder(tx, &model.Client{})
		if err != nil {
			return err
		}
		next.SortOrder = sortOrder
		report.Summary.Clients.Created++
		return tx.Create(&next).Error
	}
	switch strategy {
	case StrategySkip:
		report.warn(fmt.Sprintf("client %s: existing name skipped by strategy", agg.Email))
		return nil
	case StrategyReplace:
		next.Id = existing.Id
		next.SubSecret = existing.SubSecret
		next.SortOrder = existing.SortOrder
		if next.Links, err = buildClientLinks(tx, next.Config, next.Inbounds, hostname); err != nil {
			return err
		}
		report.Summary.Clients.Merged++
		return tx.Save(&next).Error
	default:
		mergedInbounds, err := mergeInboundJSON(existing.Inbounds, agg.Inbounds)
		if err != nil {
			return err
		}
		updates := map[string]any{"inbounds": mergedInbounds}
		mergedLinks, err := buildMergedClientLinks(tx, existing.Config, mergedInbounds, hostname, existing.Links)
		if err != nil {
			return err
		}
		if mergedLinks != nil {
			updates["links"] = mergedLinks
		}
		report.Summary.Clients.Merged++
		return tx.Model(&existing).Updates(updates).Error
	}
}

func sortStrings(values []string) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}
