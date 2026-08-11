package durableowner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/gorm"
)

type Hooks struct {
	MigrateStaged   func(context.Context, *gorm.DB) error
	RehearseRestore func(context.Context, *gorm.DB) error
}

var hookCatalog = struct {
	sync.RWMutex
	items map[string]Hooks
}{items: map[string]Hooks{}}

func RegisterHooks(ownerID string, hooks Hooks) {
	if err := manifest.ValidateID(ownerID); err != nil {
		panic(err)
	}
	hookCatalog.Lock()
	defer hookCatalog.Unlock()
	if _, duplicate := hookCatalog.items[ownerID]; duplicate {
		panic(fmt.Errorf("durable owner %q hooks already registered", ownerID))
	}
	hookCatalog.items[ownerID] = hooks
}

func RunStagedRestore(ctx context.Context, ownerID string, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return errors.New("durable owner staged restore input is unavailable")
	}
	item, available := Lookup(ownerID)
	if !available {
		return fmt.Errorf("installed durable owner %q is unavailable", ownerID)
	}
	hookCatalog.RLock()
	hooks, hooksAvailable := hookCatalog.items[ownerID]
	hookCatalog.RUnlock()
	if len(item.Database.Tables) > 0 && (!hooksAvailable || hooks.MigrateStaged == nil) {
		return fmt.Errorf("durable owner %q has tables but no staged migration hook", ownerID)
	}
	if hooks.MigrateStaged != nil {
		if err := hooks.MigrateStaged(ctx, db.WithContext(ctx)); err != nil {
			return fmt.Errorf("durable owner %q staged migration failed: %w", ownerID, err)
		}
	}
	if err := validateDeclaredResources(ctx, db, item.Database); err != nil {
		return fmt.Errorf("durable owner %q resource postcondition failed: %w", ownerID, err)
	}
	if hooks.RehearseRestore != nil {
		if err := hooks.RehearseRestore(ctx, db.WithContext(ctx)); err != nil {
			return fmt.Errorf("durable owner %q restore hook failed: %w", ownerID, err)
		}
		if err := validateDeclaredResources(ctx, db, item.Database); err != nil {
			return fmt.Errorf("durable owner %q restore postcondition failed: %w", ownerID, err)
		}
	}
	return nil
}

func validateDeclaredResources(ctx context.Context, db *gorm.DB, resources manifest.DurableResourceManifestV1) error {
	for _, table := range resources.Tables {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("declared table %q is absent", table)
		}
		var names []string
		if err := db.Raw("SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? ORDER BY name", table).Scan(&names).Error; err != nil {
			return fmt.Errorf("inspect indexes for %q: %w", table, err)
		}
		sort.Strings(names)
	}
	if len(resources.Settings)+len(resources.Secrets) > 0 {
		if !db.Migrator().HasTable("settings") {
			return errors.New("declared setting resources require the settings table")
		}
		keys := append(append([]string(nil), resources.Settings...), resources.Secrets...)
		var duplicates int64
		if err := db.Raw("SELECT COUNT(*) FROM (SELECT key FROM settings WHERE key IN ? GROUP BY key HAVING COUNT(*) > 1)", keys).Scan(&duplicates).Error; err != nil {
			return err
		}
		if duplicates != 0 {
			return errors.New("declared setting resources contain duplicate rows")
		}
	}
	for _, file := range resources.Files {
		if file.BackupClass == manifest.FileBackupOpaque {
			return fmt.Errorf("portable durable file %q has no staged archive", file.Path)
		}
	}
	return nil
}
