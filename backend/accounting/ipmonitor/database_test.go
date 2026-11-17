package ipmonitor

import (
	"context"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
)

func initializeIPDatabase(path string) error {
	return dbsqlite.InitializeContext(context.Background(), path, dbsqlite.Bootstrap{
		BeforeOpen: func() error { return nil },
		AfterOpen:  func(db *gorm.DB) error { return db.AutoMigrate(&model.Client{}, &model.ClientIP{}, &model.Setting{}) },
	})
}
