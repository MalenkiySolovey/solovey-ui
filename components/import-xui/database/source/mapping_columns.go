//go:build !minimal

package source

import "sort"

// MappingColumns exposes names only from known import-source tables. It uses
// the existing immutable read connection and never opens a writable handle.
func (s *Database) MappingColumns() (map[string][]string, error) {
	db, err := s.readDB()
	if err != nil {
		return nil, err
	}
	result := map[string][]string{}
	for _, table := range []string{"inbounds", "client_traffics", "outbound_traffics", "settings", "users"} {
		exists, err := tableExistsSQL(db, table)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		columns, err := tableColumns(db, table)
		if err != nil {
			return nil, err
		}
		for column := range columns {
			result[table] = append(result[table], column)
		}
		sort.Strings(result[table])
	}
	return result, nil
}
