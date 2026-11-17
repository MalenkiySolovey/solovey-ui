package web

import (
	"github.com/MalenkiySolovey/solovey-ui/backend/security/sessionstore"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"gorm.io/gorm"
)

type SQLiteSessionStore = sessionstore.SQLiteSessionStore

func NewSQLiteSessionStore(db *gorm.DB, keyPairs ...[]byte) (*SQLiteSessionStore, error) {
	store, err := sessionstore.NewSQLiteSessionStore(db, keyPairs...)
	if err != nil {
		return nil, err
	}
	store.CloseSession = realtime.CloseSession
	return store, nil
}
