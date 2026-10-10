package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/ssmcache"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/sagernet/sing-box/service/ssmapi"
	"gorm.io/gorm"
)

func init() {
	backup.RegisterFileOwner("ssm-cache", backup.FileOwner{Required: ssmCacheRequired, Export: exportSSMCacheFiles, Restore: restoreSSMCacheFiles, ProjectRuntime: projectSSMCacheFiles})
}

func ssmCacheReferences(ctx context.Context, db *gorm.DB) (map[string]model.Service, error) {
	refs := map[string]model.Service{}
	db = db.WithContext(ctx)
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return nil, err
	}
	present := false
	for _, name := range tables {
		if name == "services" {
			present = true
			break
		}
	}
	if !present {
		return refs, nil
	}
	var rows []model.Service
	if err := db.WithContext(ctx).Where("type = ?", "ssm-api").Limit(backup.MaxOwnerFiles).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) >= backup.MaxOwnerFiles {
		return nil, errors.New("SSM_CACHE_INVENTORY_EXCESSIVE")
	}
	for _, row := range rows {
		name, err := ssmcache.Path(row.Options)
		if err != nil {
			return nil, err
		}
		if name != "" {
			refs["service:"+strconv.FormatUint(uint64(row.Id), 10)] = row
		}
	}
	return refs, nil
}

func ssmCacheRequired(ctx context.Context, db *gorm.DB) (bool, error) {
	refs, err := ssmCacheReferences(ctx, db)
	return len(refs) > 0, err
}

type ssmCacheInventory struct {
	Schema  string          `json:"schema"`
	Present map[string]bool `json:"present"`
}

func exportSSMCacheFiles(ctx context.Context, db *gorm.DB) ([]backup.OwnerFile, error) {
	refs, err := ssmCacheReferences(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}
	manifest := ssmCacheInventory{Schema: "solovey.ssm-cache-files/v1", Present: map[string]bool{}}
	files := []backup.OwnerFile{}
	for key, row := range refs {
		name, _ := ssmcache.Path(row.Options)
		store, err := ssmcache.New(name)
		if err != nil {
			return nil, err
		}
		data, err := store.Read(ctx)
		if errors.Is(err, os.ErrNotExist) {
			manifest.Present[key] = false
			continue
		}
		if err != nil {
			return nil, err
		}
		manifest.Present[key] = true
		files = append(files, backup.OwnerFile{Key: key, Data: data})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	return append(files, backup.OwnerFile{Key: "inventory", Data: data}), nil
}

func validateSSMCacheFiles(ctx context.Context, db *gorm.DB, files []backup.OwnerFile) (map[string]model.Service, map[string][]byte, error) {
	refs, err := ssmCacheReferences(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	if len(refs) == 0 {
		if len(files) != 0 {
			return nil, nil, errors.New("SSM_CACHE_INVENTORY_INVALID")
		}
		return refs, nil, nil
	}
	var inventory ssmCacheInventory
	data := map[string][]byte{}
	seen := false
	for _, file := range files {
		if file.Key == "inventory" {
			if seen || json.Unmarshal(file.Data, &inventory) != nil || inventory.Schema != "solovey.ssm-cache-files/v1" {
				return nil, nil, errors.New("SSM_CACHE_INVENTORY_INVALID")
			}
			seen = true
			continue
		}
		if _, ok := refs[file.Key]; !ok {
			return nil, nil, errors.New("SSM_CACHE_INVENTORY_INVALID")
		}
		if _, duplicate := data[file.Key]; duplicate {
			return nil, nil, errors.New("SSM_CACHE_INVENTORY_INVALID")
		}
		if err = ssmapi.ValidateCache(file.Data); err != nil {
			return nil, nil, err
		}
		data[file.Key] = file.Data
	}
	if !seen || len(inventory.Present) != len(refs) {
		return nil, nil, errors.New("SSM_CACHE_INVENTORY_MISSING")
	}
	for key := range refs {
		present, ok := inventory.Present[key]
		if !ok {
			return nil, nil, errors.New("SSM_CACHE_INVENTORY_MISSING")
		}
		_, have := data[key]
		if present != have {
			return nil, nil, errors.New("SSM_CACHE_INVENTORY_MISSING")
		}
		if !present {
			data[key] = []byte("{}")
		} // explicit absent-cache default
	}
	return refs, data, nil
}

func restoreSSMCacheFiles(ctx context.Context, db *gorm.DB, files []backup.OwnerFile, publish bool) error {
	return db.WithContext(ctx).Transaction(func(candidate *gorm.DB) error {
		refs, data, err := validateSSMCacheFiles(ctx, candidate, files)
		if err != nil {
			return err
		}
		for key, row := range refs {
			var folder string
			if !publish {
				id, err := common.RandomUUID()
				if err != nil {
					return errors.New("SSM_CACHE_RESTORE_IDENTITY_FAILED")
				}
				folder = filepath.Join(ssmcache.Root(), "restored", id)
			} else {
				name, _ := ssmcache.Path(row.Options)
				rel, err := filepath.Rel(ssmcache.Root(), name)
				parts := strings.Split(filepath.ToSlash(rel), "/")
				if err != nil || len(parts) != 4 || parts[0] != "restored" || len(parts[1]) != 36 || parts[3] != "state.seed" {
					return errors.New("SSM_CACHE_RESTORE_NOT_PREPARED")
				}
				folder = filepath.Join(ssmcache.Root(), "restored", parts[1])
			}
			generation, err := backup.PublishOwnedTree(folder, map[string][]byte{"state.seed": data[key]}, false)
			if err != nil {
				return errors.New("SSM_CACHE_RESTORE_INVALID")
			}
			name := filepath.Join(generation, "state.seed")
			if publish {
				// Create/prove the private owner root before the existing immutable
				// publisher creates descendants. No live cache is overwritten.
				store, err := ssmcache.New(name)
				if err != nil {
					return err
				}
				if err = store.PreparePrivate(); err != nil {
					return err
				}
				if _, err = backup.PublishOwnedTree(folder, map[string][]byte{"state.seed": data[key]}, true); err != nil {
					return errors.New("SSM_CACHE_RESTORE_PUBLISH_FAILED")
				}
			}
			var options map[string]json.RawMessage
			if json.Unmarshal(row.Options, &options) != nil {
				return errors.New("SSM_CACHE_OPTIONS_INVALID")
			}
			options["cache_path"], _ = json.Marshal(name)
			raw, err := json.Marshal(options)
			if err != nil {
				return err
			}
			if err = candidate.Model(&model.Service{}).Where("id = ?", row.Id).Update("options", json.RawMessage(raw)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func projectSSMCacheFiles(ctx context.Context, db *gorm.DB, files []backup.OwnerFile, config []byte) ([]byte, error) {
	if _, _, err := validateSSMCacheFiles(ctx, db, files); err != nil {
		return config, err
	}
	// The pinned dry constructor does no cache I/O. Validated staged paths are
	// sufficient; no cache contents enter exported config or Doctor findings.
	return config, nil
}
