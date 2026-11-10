//go:build !minimal

// Package observability binds transient history to the Panel database lifecycle.
// The independent component owns all history, retention and aggregation policy.
package observability

import (
	"sync"

	canonical "github.com/MalenkiySolovey/solovey-component-observability-extra/history"
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/gorm"
)

type Service = canonical.Service
type Store = canonical.Store

var sharedHistory struct {
	sync.Mutex
	store    *Store
	database *gorm.DB
}

// SharedStore preserves the legacy one-local-host facade without eager allocation.
// A database replacement while the component is inactive invalidates old history.
func SharedStore() *Store {
	sharedHistory.Lock()
	defer sharedHistory.Unlock()
	database := dbsqlite.DB()
	if sharedHistory.store == nil {
		sharedHistory.store = canonical.NewStore()
	} else if sharedHistory.database != database {
		sharedHistory.store.Reset()
	}
	sharedHistory.database = database
	return sharedHistory.store
}

func resetHistory() {
	sharedHistory.Lock()
	defer sharedHistory.Unlock()
	if sharedHistory.store != nil {
		sharedHistory.store.Reset()
	}
	sharedHistory.database = dbsqlite.DB()
}

// BindDatabaseReset attaches the existing Backend-owned hook only while enabled.
// ComponentHost serializes admission and draining; a failed stop retains this hook.
func BindDatabaseReset() func() {
	dbhooks.RegisterResetHook("service.observability", resetHistory)
	return func() { dbhooks.RegisterResetHook("service.observability", nil) }
}
