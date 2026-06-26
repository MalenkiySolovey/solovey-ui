package sqlite

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

var adaptToCurrentVersion = adapt

func Init(dbPath string) error {
	if err := open(dbPath); err != nil {
		return err
	}
	if err := ensureDefaultOutbound(gormDefaultOutboundStore{db: db}); err != nil {
		return err
	}
	if err := db.AutoMigrate(schemaModels()...); err != nil {
		return err
	}
	if err := dropDeprecatedTables(); err != nil {
		return err
	}
	if err := ensureNoTLSRow(); err != nil {
		return err
	}
	if err := ensureIndexes(); err != nil {
		return err
	}
	if err := ensureInitialAdmin(dbPath); err != nil {
		return err
	}
	if err := adaptToCurrentVersion(); err != nil {
		return fmt.Errorf("post-migration adapt failed: %w", err)
	}
	if err := ensureSortOrders(); err != nil {
		return fmt.Errorf("sort-order backfill failed: %w", err)
	}
	return nil
}

func schemaModels() []any {
	return []any{
		&model.Setting{}, &model.Tls{}, &model.Inbound{}, &model.Outbound{},
		&model.RemoteOutboundSubscription{}, &model.RemoteOutboundGroup{},
		&model.RemoteOutboundGroupConnection{}, &model.RemoteOutboundConnection{},
		&model.Service{}, &model.Endpoint{}, &model.User{}, &model.Tokens{},
		&model.Stats{}, &model.ClientIP{}, &model.Client{}, &model.Changes{},
		&model.AuditEvent{}, &model.FailoverMemberState{},
	}
}

func ensureInitialAdmin(dbPath string) error {
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	passwordPath := initialAdminPasswordPath(dbPath)
	if count != 0 {
		warnIfInitialAdminPasswordFileExists(passwordPath)
		return nil
	}

	password := common.Random(24)
	passwordHash, err := common.HashPassword(password)
	if err != nil {
		return err
	}
	if err := writeInitialAdminPassword(passwordPath, password); err != nil {
		return err
	}
	if err := db.Create(&model.User{Username: "admin", Password: passwordHash}).Error; err != nil {
		_ = os.Remove(passwordPath)
		return err
	}
	notifyInitialAdminPasswordSaved(passwordPath)
	return nil
}

type defaultOutboundStore interface {
	HasTable(value any) bool
	CreateTable(values ...any) error
	Create(value any) error
}

type gormDefaultOutboundStore struct{ db *gorm.DB }

func (s gormDefaultOutboundStore) HasTable(value any) bool { return s.db.Migrator().HasTable(value) }
func (s gormDefaultOutboundStore) CreateTable(values ...any) error {
	return s.db.Migrator().CreateTable(values...)
}
func (s gormDefaultOutboundStore) Create(value any) error { return s.db.Create(value).Error }

func ensureDefaultOutbound(store defaultOutboundStore) error {
	if store.HasTable(&model.Outbound{}) {
		return nil
	}
	if err := store.CreateTable(&model.Outbound{}); err != nil {
		return err
	}
	return store.Create(&[]model.Outbound{{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)}})
}
