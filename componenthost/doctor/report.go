package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"

	"gorm.io/gorm"
)

type Report struct {
	InstalledPath   string
	MetadataPresent bool
	MetadataBinary  string
	MetadataError   string
	Rows            []Row
	Issues          []Issue
}

type Row struct {
	ID                string
	Name              string
	Version           string
	Delivery          string
	Available         bool
	Installed         bool
	Enabled           bool
	Active            bool
	DefaultEnabled    bool
	InstalledSource   string
	MigrationVersions []string
	Issues            []Issue
}

type Issue struct {
	Severity string
	Message  string
}

type Options struct {
	Components    []registry.Component
	InstalledPath string
	DB            *gorm.DB
}

func Inspect(db *gorm.DB) Report {
	return InspectWith(Options{
		InstalledPath: installstate.DefaultPath(),
		DB:            db,
	})
}

func InspectWith(options Options) Report {
	report := Report{
		InstalledPath: options.InstalledPath,
	}
	if report.InstalledPath == "" {
		report.Issues = append(report.Issues, Issue{Severity: "error", Message: "installed metadata path is empty"})
	}

	metadata, metadataPresent, metadataErr := installstate.Load(options.InstalledPath)
	report.MetadataPresent = metadataPresent
	report.MetadataBinary = metadata.Binary
	if metadataErr != nil {
		report.MetadataError = metadataErr.Error()
		report.Issues = append(report.Issues, Issue{Severity: "error", Message: "installed metadata is invalid: " + metadataErr.Error()})
	}

	installed := map[string]installstate.InstalledComponent{}
	metadataIDs := map[string]struct{}{}
	componentsDir := filepath.Dir(options.InstalledPath)
	if metadataPresent && metadataErr == nil {
		for _, item := range metadata.Components {
			metadataIDs[item.ID] = struct{}{}
			installed[item.ID] = item
		}
	}

	migrations := componentMigrations(options.DB)
	enabledSettings, invalidEnabledSettings := componentEnabledSettings(options.DB)
	for _, key := range invalidEnabledSettings {
		report.Issues = append(report.Issues, Issue{
			Severity: "warn",
			Message:  "component enabled setting key is invalid: " + key,
		})
	}
	allIDs := map[string]struct{}{}
	for id := range metadataIDs {
		allIDs[id] = struct{}{}
	}
	for id := range migrations {
		allIDs[id] = struct{}{}
	}
	for id := range enabledSettings {
		allIDs[id] = struct{}{}
	}

	ids := make([]string, 0, len(allIDs))
	for id := range allIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	components := options.Components
	if components == nil {
		components = registry.ComponentsByID(ids)
	}
	available := map[string]manifest.Manifest{}
	for _, component := range components {
		available[component.Manifest.ID] = component.Manifest
	}

	for _, id := range ids {
		row := Row{ID: id}
		item, isAvailable := available[id]
		row.Available = isAvailable
		if isAvailable {
			row.Name = item.Name
			row.Version = item.Version
			row.Delivery = string(item.Delivery)
			row.DefaultEnabled = item.DefaultEnabled
		}

		if metadataPresent && metadataErr == nil {
			metaItem, mentioned := installed[id]
			if mentioned {
				row.Installed = metaItem.Installed
				row.InstalledSource = "metadata"
				if row.Delivery == "" {
					row.Delivery = string(metaItem.Delivery)
				}
				if err := manifest.ValidateID(metaItem.ID); err != nil {
					row.Issues = append(row.Issues, Issue{Severity: "error", Message: err.Error()})
				}
				if metaItem.Installed {
					row.Issues = append(row.Issues, metadataIssues(item, isAvailable, metaItem)...)
					row.Issues = append(row.Issues, packIssues(componentsDir, item, isAvailable, metaItem)...)
				}
			} else {
				row.InstalledSource = "metadata"
			}
		} else {
			row.InstalledSource = "unknown"
		}

		row.MigrationVersions = migrations[id]
		if len(row.MigrationVersions) > 0 && !row.Installed {
			row.Issues = append(row.Issues, Issue{
				Severity: "warn",
				Message:  "component migration data exists but the component is not installed",
			})
		}
		if len(row.MigrationVersions) > 0 && !row.Available {
			row.Issues = append(row.Issues, Issue{
				Severity: "warn",
				Message:  "component migration data exists but the component is unavailable in this binary",
			})
		}

		_, hasEnabledSetting := enabledSettings[id]
		enabled, enabledIssue := componentEnabled(options.DB, item, row.Available)
		row.Enabled = enabled
		if !row.Installed && !hasEnabledSetting {
			row.Enabled = false
		}
		if enabledIssue != nil {
			row.Issues = append(row.Issues, *enabledIssue)
		}
		if rawEnabled, ok := enabledSettings[id]; ok {
			if !row.Available {
				if _, err := strconv.ParseBool(rawEnabled); err != nil {
					row.Issues = append(row.Issues, Issue{Severity: "error", Message: "enabled setting is invalid: " + err.Error()})
				}
				row.Issues = append(row.Issues, Issue{
					Severity: "warn",
					Message:  "component enabled setting exists but the component is unavailable in this binary",
				})
			}
		}
		if row.Enabled && !row.Installed {
			row.Issues = append(row.Issues, Issue{
				Severity: "warn",
				Message:  "component enabled setting is true but the component is not installed",
			})
		}
		row.Active = row.Available && row.Installed && row.Enabled
		report.Rows = append(report.Rows, row)
		report.Issues = append(report.Issues, row.Issues...)
	}

	sort.SliceStable(report.Rows, func(i, j int) bool {
		if report.Rows[i].Active != report.Rows[j].Active {
			return report.Rows[i].Active
		}
		return report.Rows[i].ID < report.Rows[j].ID
	})
	return report
}

func HasErrors(report Report) bool {
	for _, issue := range report.Issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}

func packIssues(componentsDir string, item manifest.Manifest, isAvailable bool, metaItem installstate.InstalledComponent) []Issue {
	var issues []Issue
	if componentsDir == "" {
		return []Issue{{Severity: "error", Message: "component pack directory is unknown"}}
	}
	packDir := filepath.Join(componentsDir, metaItem.ID)
	info, err := os.Stat(packDir)
	if err != nil {
		return []Issue{{Severity: "error", Message: "installed component pack is missing: " + err.Error()}}
	}
	if !info.IsDir() {
		return []Issue{{Severity: "error", Message: "installed component pack path is not a directory: " + packDir}}
	}
	if _, err := os.Stat(filepath.Join(packDir, "component.json")); err != nil {
		issues = append(issues, Issue{Severity: "error", Message: "installed component pack manifest is missing: " + err.Error()})
	}
	if isAvailable && len(item.Frontend.Entries) > 0 {
		assetDir := filepath.Join(packDir, "frontend", "assets")
		info, err := os.Stat(assetDir)
		if err != nil {
			issues = append(issues, Issue{Severity: "error", Message: "installed component frontend assets are missing: " + err.Error()})
		} else if !info.IsDir() {
			issues = append(issues, Issue{Severity: "error", Message: "installed component frontend asset path is not a directory: " + assetDir})
		}
	}
	return issues
}

func metadataIssues(item manifest.Manifest, isAvailable bool, metaItem installstate.InstalledComponent) []Issue {
	var issues []Issue
	if !isAvailable {
		issues = append(issues, Issue{
			Severity: "error",
			Message:  "installed metadata references a component unavailable in this binary",
		})
		return issues
	}
	if metaItem.Delivery != "" && metaItem.Delivery != item.Delivery {
		issues = append(issues, Issue{
			Severity: "error",
			Message: fmt.Sprintf(
				"installed delivery %q does not match binary delivery %q",
				metaItem.Delivery,
				item.Delivery,
			),
		})
	}
	return issues
}

func componentEnabled(db *gorm.DB, item manifest.Manifest, available bool) (bool, *Issue) {
	if !available {
		return false, nil
	}
	if db == nil {
		return item.DefaultEnabled, nil
	}
	if !db.Migrator().HasTable(&model.Setting{}) {
		return item.DefaultEnabled, nil
	}
	var setting model.Setting
	err := db.Model(model.Setting{}).Where("key = ?", enabledstate.SettingKey(item.ID)).First(&setting).Error
	if dbsqlite.IsNotFound(err) {
		return item.DefaultEnabled, nil
	}
	if err != nil {
		return false, &Issue{Severity: "error", Message: "failed to read enabled setting: " + err.Error()}
	}
	enabled, err := strconv.ParseBool(setting.Value)
	if err != nil {
		return false, &Issue{Severity: "error", Message: "enabled setting is invalid: " + err.Error()}
	}
	return enabled, nil
}

func componentMigrations(db *gorm.DB) map[string][]string {
	result := map[string][]string{}
	if db == nil || !db.Migrator().HasTable(&model.ComponentMigration{}) {
		return result
	}
	var rows []model.ComponentMigration
	if err := db.Order("component_id asc, version asc").Find(&rows).Error; err != nil {
		return result
	}
	for _, row := range rows {
		result[row.ComponentID] = append(result[row.ComponentID], row.Version)
	}
	return result
}

func componentEnabledSettings(db *gorm.DB) (map[string]string, []string) {
	result := map[string]string{}
	if db == nil || !db.Migrator().HasTable(&model.Setting{}) {
		return result, nil
	}
	var rows []model.Setting
	if err := db.Model(model.Setting{}).Where(`"key" LIKE ?`, "%.enabled").Find(&rows).Error; err != nil {
		return result, nil
	}
	var invalid []string
	for _, row := range rows {
		id, ok := enabledstate.ComponentIDFromSettingKey(row.Key)
		if !ok {
			invalid = append(invalid, row.Key)
			continue
		}
		result[id] = row.Value
	}
	sort.Strings(invalid)
	return result, invalid
}
