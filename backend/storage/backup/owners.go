package backup

import (
	"errors"
	"fmt"
	"sort"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/durableowner"
	"gorm.io/gorm"
)

func (e Exporter) installedOwnerTables(sourceDB *gorm.DB, already map[string]struct{}) ([]Table, error) {
	if e.InstalledOwners == nil {
		return nil, errors.New("installed owner manifest is unavailable")
	}
	installed, err := e.InstalledOwners()
	if err != nil {
		return nil, fmt.Errorf("installed owner manifest is unavailable: %w", err)
	}
	registered := registeredTablesByName()
	claimed := map[string]string{}
	result := []Table{}
	for _, owner := range installed {
		component, available := durableowner.Lookup(owner)
		if !available {
			return nil, fmt.Errorf("installed durable owner %q is unavailable; backup fails closed", owner)
		}
		excluded, err := durableowner.NonportableBackupTables(owner)
		if err != nil {
			return nil, err
		}
		for _, tableName := range component.Database.Tables {
			if previous := claimed[tableName]; previous != "" && previous != owner {
				return nil, fmt.Errorf("durable table %q has ambiguous owners %q and %q", tableName, previous, owner)
			}
			claimed[tableName] = owner
			if _, exists := already[tableName]; exists {
				return nil, fmt.Errorf("durable table %q claimed by %q conflicts with an existing backup owner", tableName, owner)
			}
			if excluded[tableName] {
				result = append(result, Table{Name: tableName, Owner: owner, AlwaysExclude: true, ExclusionCode: "NONPORTABLE_HOST_AUTHORITY"})
				already[tableName] = struct{}{}
				continue
			}
			if !sourceDB.Migrator().HasTable(tableName) {
				continue
			}
			if typed, ok := registered[tableName]; ok {
				if typed.Owner == "AMBIGUOUS" || typed.Owner != owner {
					return nil, fmt.Errorf("durable table %q registration owner mismatch", tableName)
				}
				result = append(result, typed)
			} else {
				result = append(result, Table{Name: tableName, Owner: owner, Opaque: true})
			}
			already[tableName] = struct{}{}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Owner == result[j].Owner {
			return result[i].Name < result[j].Name
		}
		return result[i].Owner < result[j].Owner
	})
	return result, nil
}

func registeredTablesByName() map[string]Table {
	backupTableRegistry.RLock()
	defer backupTableRegistry.RUnlock()
	result := make(map[string]Table, len(backupTableRegistry.tables))
	for _, registered := range backupTableRegistry.tables {
		if existing, duplicate := result[registered.table.Name]; duplicate && existing.Owner != registered.table.Owner {
			result[registered.table.Name] = Table{Name: registered.table.Name, Owner: "AMBIGUOUS"}
			continue
		}
		result[registered.table.Name] = registered.table
	}
	return result
}

var ErrOwnerUnavailable = errors.New("installed durable owner is unavailable")
