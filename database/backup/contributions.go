package backup

import (
	"strings"
	"sync"

	"gorm.io/gorm"
)

// TableContribution describes a component-owned table that should be included
// in database backups while the owning component is active.
type TableContribution struct {
	Name  string
	Model any
}

type registeredBackupTable struct {
	token uint64
	table backupTable
}

var backupTableRegistry = struct {
	sync.RWMutex
	next   uint64
	tables []registeredBackupTable
}{}

func RegisterTables(owner string, tables []TableContribution) func() {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return func() {}
	}
	registered := make([]registeredBackupTable, 0, len(tables))
	for _, table := range tables {
		name := strings.TrimSpace(table.Name)
		if name == "" || table.Model == nil {
			continue
		}
		registered = append(registered, registeredBackupTable{
			table: backupTable{name: name, model: table.Model},
		})
	}
	if len(registered) == 0 {
		return func() {}
	}

	backupTableRegistry.Lock()
	backupTableRegistry.next++
	token := backupTableRegistry.next
	for i := range registered {
		registered[i].token = token
	}
	backupTableRegistry.tables = append(backupTableRegistry.tables, registered...)
	backupTableRegistry.Unlock()

	return func() {
		backupTableRegistry.Lock()
		defer backupTableRegistry.Unlock()
		dst := backupTableRegistry.tables[:0]
		for _, table := range backupTableRegistry.tables {
			if table.token != token {
				dst = append(dst, table)
			}
		}
		backupTableRegistry.tables = dst
	}
}

func contributedBackupTables(sourceDB *gorm.DB) []backupTable {
	if sourceDB == nil {
		return nil
	}
	backupTableRegistry.RLock()
	registered := append([]registeredBackupTable(nil), backupTableRegistry.tables...)
	backupTableRegistry.RUnlock()

	tables := make([]backupTable, 0, len(registered))
	seen := make(map[string]struct{}, len(registered))
	for _, registeredTable := range registered {
		table := registeredTable.table
		if _, ok := seen[table.name]; ok {
			continue
		}
		seen[table.name] = struct{}{}
		if sourceDB.Migrator().HasTable(table.name) {
			tables = append(tables, table)
		}
	}
	return tables
}
