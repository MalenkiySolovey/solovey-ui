package backup

import (
	"errors"
	"fmt"
	"sort"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"gorm.io/gorm"
)

func installedOwnerTables(sourceDB *gorm.DB, already map[string]struct{}) ([]backupTable, error) {
	installed, err := installstate.InstalledComponents()
	if err != nil {
		return nil, fmt.Errorf("installed owner manifest is unavailable: %w", err)
	}
	registered := registeredTablesByName()
	claimed := map[string]string{}
	result := []backupTable{}
	for _, owner := range installed {
		component, available := durableowner.Lookup(owner.ID)
		if !available {
			return nil, fmt.Errorf("installed durable owner %q is unavailable; backup fails closed", owner.ID)
		}
		for _, tableName := range component.Database.Tables {
			if previous := claimed[tableName]; previous != "" && previous != owner.ID {
				return nil, fmt.Errorf("durable table %q has ambiguous owners %q and %q", tableName, previous, owner.ID)
			}
			claimed[tableName] = owner.ID
			if _, exists := already[tableName]; exists {
				return nil, fmt.Errorf("durable table %q claimed by %q conflicts with an existing backup owner", tableName, owner.ID)
			}
			if !sourceDB.Migrator().HasTable(tableName) {
				continue
			}
			if typed, ok := registered[tableName]; ok {
				if typed.owner == "AMBIGUOUS" || typed.owner != owner.ID {
					return nil, fmt.Errorf("durable table %q registration owner mismatch", tableName)
				}
				result = append(result, typed)
			} else {
				result = append(result, backupTable{name: tableName, owner: owner.ID, opaque: true})
			}
			already[tableName] = struct{}{}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].owner == result[j].owner {
			return result[i].name < result[j].name
		}
		return result[i].owner < result[j].owner
	})
	return result, nil
}

func registeredTablesByName() map[string]backupTable {
	backupTableRegistry.RLock()
	defer backupTableRegistry.RUnlock()
	result := make(map[string]backupTable, len(backupTableRegistry.tables))
	for _, registered := range backupTableRegistry.tables {
		if existing, duplicate := result[registered.table.name]; duplicate && existing.owner != registered.table.owner {
			result[registered.table.name] = backupTable{name: registered.table.name, owner: "AMBIGUOUS"}
			continue
		}
		result[registered.table.name] = registered.table
	}
	return result
}

var ErrOwnerUnavailable = errors.New("installed durable owner is unavailable")
