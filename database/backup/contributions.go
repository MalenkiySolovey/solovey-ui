package backup

import (
	"strings"
	"sync"
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

const maxContributedBackupTables = 1024

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
			table: backupTable{name: name, model: table.Model, owner: owner},
		})
	}
	if len(registered) == 0 {
		return func() {}
	}

	backupTableRegistry.Lock()
	if len(backupTableRegistry.tables)+len(registered) > maxContributedBackupTables {
		backupTableRegistry.Unlock()
		panic("backup table contribution registry capacity exceeded")
	}
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
