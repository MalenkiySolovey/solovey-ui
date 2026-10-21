//go:build !minimal

package importxui

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"gorm.io/gorm"
)

func applyClient(tx *gorm.DB, agg *clientAggregate, strategy Strategy, report *Report, hostname string) error {
	next, err := agg.toModel()
	if err != nil {
		return err
	}
	var existing model.Client
	err = tx.Where("name = ?", agg.Email).First(&existing).Error
	if err != nil && !dbsqlite.IsNotFound(err) {
		return err
	}
	action := "new"
	if err == nil {
		if strategy == StrategySkip {
			report.warn(fmt.Sprintf("client %s: existing name skipped by strategy", agg.Email))
			return nil
		}
		if strategy != StrategyReplace {
			added := make([]uint, 0, len(agg.Inbounds))
			for id := range agg.Inbounds {
				added = append(added, id)
			}
			if err := entityclients.MergeImportedMembership(tx, existing.Id, added, hostname); err != nil {
				return err
			}
			report.Summary.Clients.Merged++
			return nil
		}
		next.Id = existing.Id
		next.SubSecret = existing.SubSecret
		next.SortOrder = existing.SortOrder
		action = "edit"
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if _, err := entityclients.SaveImportedCandidate(entityclients.SaveRequest{Tx: tx, Action: action, Data: raw, Hostname: hostname}); err != nil {
		return err
	}
	if action == "new" {
		report.Summary.Clients.Created++
	} else {
		report.Summary.Clients.Merged++
	}
	return nil
}

func sortStrings(values []string) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}
