package backup

import canonical "github.com/MalenkiySolovey/solovey-backend/storage/backup"

type TableContribution = canonical.TableContribution

func RegisterTables(owner string, tables []TableContribution) func() {
	return canonical.RegisterTables(owner, tables)
}
