//go:build !minimal

package blockcheck

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/settings/store"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-host/componenthost/durable"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errStorage = errors.New("blockcheck_catalog_storage_unavailable")

// Reserve the SQLite writer before reading a revision or migrating storage.
// Another Panel writer could otherwise invalidate a deferred WAL snapshot.
func writeStoredBlockCheck(ctx context.Context, db *gorm.DB, storageErr error, fn func(*gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE settings SET value = value WHERE 0").Error; err != nil {
			return storageErr
		}
		return fn(tx)
	})
}

type catalogGroup struct {
	ID       string `gorm:"primaryKey"`
	Position int    `gorm:"not null;index"`
	Disabled bool   `gorm:"not null"`
	Payload  string `gorm:"not null"`
}

func (catalogGroup) TableName() string { return feature.CatalogGroupTable }

func catalogSetting(tx *gorm.DB, key string) (string, error) {
	var row model.Setting
	result := tx.Where("key = ?", key).Limit(1).Find(&row)
	if result.Error != nil || result.RowsAffected != 0 && row.Value == "" {
		return "", errStorage
	}
	return row.Value, nil
}

func catalogHasRows(tx *gorm.DB) (bool, error) {
	var ids []string
	result := tx.Model(&catalogGroup{}).Limit(1).Pluck("id", &ids)
	if result.Error != nil {
		return false, errStorage
	}
	return len(ids) != 0, nil
}

func readDefaultCatalog(tx *gorm.DB) (feature.Catalog, error) {
	// No revision means defaults only if neither storage format holds data.
	legacy, err := catalogSetting(tx, feature.CatalogSettingKey)
	if err != nil {
		return feature.Catalog{}, err
	}
	hasRows, err := catalogHasRows(tx)
	if err != nil {
		return feature.Catalog{}, err
	}
	if legacy != "" || hasRows {
		return feature.Catalog{}, feature.ErrInvalidCatalog
	}
	return feature.DecodeCatalog(nil)
}

func loadGroups(query *gorm.DB, full bool) ([]feature.Group, error) {
	rows, err := query.Model(&catalogGroup{}).Order("position ASC, id ASC").Rows()
	if err != nil {
		return nil, errStorage
	}
	defer rows.Close()
	groups := []feature.Group{}
	for rows.Next() {
		var row catalogGroup
		if err := query.ScanRows(rows, &row); err != nil {
			return nil, errStorage
		}
		entry, err := feature.DecodeGroup([]byte(row.Payload))
		if err != nil || entry.ID != row.ID || entry.Disabled != row.Disabled || full && row.Position != len(groups) {
			return nil, feature.ErrInvalidCatalog
		}
		groups = append(groups, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, errStorage
	}
	return groups, nil
}

func loadCatalog(tx *gorm.DB) (feature.Catalog, string, error) {
	revision, err := catalogSetting(tx, feature.CatalogRevisionSettingKey)
	if err != nil {
		return feature.Catalog{}, "", err
	}
	if revision == "" {
		catalog, err := readDefaultCatalog(tx)
		return catalog, "", err
	}
	groups, err := loadGroups(tx, true)
	if err != nil {
		return feature.Catalog{}, "", err
	}
	catalog := feature.Catalog{Schema: feature.CatalogSchema, Groups: groups}
	if catalog.Revision() != revision {
		return feature.Catalog{}, "", feature.ErrInvalidCatalog
	}
	return catalog, revision, nil
}

func readCatalog(ctx context.Context, db *gorm.DB) (catalog feature.Catalog, err error) {
	if db == nil {
		return catalog, errStorage
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		catalog, _, err = loadCatalog(tx)
		return err
	})
	return catalog, err
}

func readRunCatalog(ctx context.Context, db *gorm.DB, expected, id string, all bool) (catalog feature.Catalog, err error) {
	if db == nil {
		return catalog, errStorage
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		revision, err := catalogSetting(tx, feature.CatalogRevisionSettingKey)
		if err != nil {
			return err
		}
		groups := []feature.Group{}
		if revision == "" {
			defaults, err := readDefaultCatalog(tx)
			if err != nil {
				return err
			}
			revision = defaults.Revision()
			for _, group := range defaults.Groups {
				if (all && !group.Disabled) || (!all && group.ID == id) {
					groups = append(groups, group)
				}
			}
		} else if revision == expected {
			query := tx
			if all {
				query = query.Where("disabled = ?", false)
			} else {
				query = query.Where("id = ?", id)
			}
			groups, err = loadGroups(query, false)
			if err != nil {
				return err
			}
		}
		if expected != revision {
			return feature.ErrStaleCatalog
		}
		catalog, err = feature.SelectedCatalog(revision, groups)
		return err
	})
	return catalog, err
}

func changeCatalog(ctx context.Context, db *gorm.DB, request feature.CatalogChange) (catalog feature.Catalog, err error) {
	if db == nil {
		return catalog, errStorage
	}
	err = writeStoredBlockCheck(ctx, db, errStorage, func(tx *gorm.DB) error {
		current, revision, err := loadCatalog(tx)
		if err != nil {
			return err
		}
		catalog, err = current.Apply(request)
		if err != nil {
			return err
		}
		prior := make(map[string]int, len(current.Groups))
		if revision != "" {
			for i, group := range current.Groups {
				prior[group.ID] = i
			}
		}
		for i, group := range catalog.Groups {
			oldPosition, exists := prior[group.ID]
			delete(prior, group.ID)
			if exists && oldPosition == i && current.Groups[i].Revision() == group.Revision() {
				continue
			}
			if err := writeCatalogGroup(tx, i, group); err != nil {
				return err
			}
		}
		for id := range prior {
			if err := tx.Where("id = ?", id).Delete(&catalogGroup{}).Error; err != nil {
				return errStorage
			}
		}
		if err := store.UpsertValue(tx, feature.CatalogRevisionSettingKey, catalog.Revision()); err != nil {
			return errStorage
		}
		return nil
	})
	return catalog, err
}

func writeCatalogGroup(tx *gorm.DB, position int, group feature.Group) error {
	data, err := json.Marshal(group)
	if err != nil {
		return feature.ErrInvalidGroup
	}
	row := catalogGroup{ID: group.ID, Position: position, Disabled: group.Disabled, Payload: string(data)}
	if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		return errStorage
	}
	return nil
}

func migrateStorage(ctx context.Context, db *gorm.DB) error {
	return writeStoredBlockCheck(ctx, db, errStorage, func(tx *gorm.DB) error {
		if err := migrateCatalogRows(tx); err != nil {
			return err
		}
		return migrateHistory(tx, time.Now())
	})
}

func migrateCatalogRows(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&catalogGroup{}); err != nil {
		return err
	}
	legacy, err := catalogSetting(tx, feature.CatalogSettingKey)
	if err != nil {
		return err
	}
	if legacy == "" {
		_, _, err := loadCatalog(tx)
		return err
	}
	existing, err := catalogSetting(tx, feature.CatalogRevisionSettingKey)
	if err != nil {
		return err
	}
	hasRows, err := catalogHasRows(tx)
	if err != nil {
		return err
	}
	if existing != "" || hasRows {
		return feature.ErrInvalidCatalog
	}
	catalog, err := feature.DecodeCatalog([]byte(legacy))
	if err != nil {
		return err
	}
	for i, group := range catalog.Groups {
		if err := writeCatalogGroup(tx, i, group); err != nil {
			return err
		}
	}
	if err := store.UpsertValue(tx, feature.CatalogRevisionSettingKey, catalog.Revision()); err != nil {
		return err
	}
	return tx.Where("key = ?", feature.CatalogSettingKey).Delete(&model.Setting{}).Error
}

func inspectCatalogDrop(context.Context, *gorm.DB, time.Time) durable.AuthorityStatus {
	return durable.AuthorityStatus{State: "VERIFIED_SAFE"}
}

func dropCatalog(_ context.Context, db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Migrator().DropTable(&historyGroupRow{}, &historyRunRow{}, &catalogGroup{}); err != nil {
			return err
		}
		return tx.Where("key IN ?", []string{feature.CatalogSettingKey, feature.CatalogRevisionSettingKey, feature.HistorySettingKey}).Delete(&model.Setting{}).Error
	})
}
