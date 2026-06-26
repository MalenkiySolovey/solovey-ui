package service

import (
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	servicefailover "github.com/MalenkiySolovey/solovey-ui/service/failover"
)

// FailoverStatusEntries returns the live status of every failover group. It
// sources the database and the running sing-box core from the default runtime,
// so transport layers can expose failover status without importing the database
// package directly.
func FailoverStatusEntries() ([]servicefailover.StatusEntry, error) {
	return servicefailover.Status(dbsqlite.DB(), DefaultRuntime().Core())
}
