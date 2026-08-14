//go:build !minimal

package domain

import "gorm.io/gorm"

func EnsureSchema(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if err := db.AutoMigrate(
		&Site{},
		&Page{},
		&Redirect{},
		&Asset{},
		&Publish{},
		&PublishFile{},
		&PublishRedirect{},
		&SafetyReport{},
		&TemplateSource{},
		&SelfStealDraft{},
		&RuntimeTarget{},
		&ExternalResource{},
		&Event{},
	); err != nil {
		return err
	}
	// Direct node mutation was never part of the public component surface. Drop
	// its unreachable persistence, including plaintext endpoint secrets.
	for _, retired := range []string{"fallback_html_node_publications", "fallback_html_node_endpoints"} {
		if db.Migrator().HasTable(retired) {
			if err := db.Migrator().DropTable(retired); err != nil {
				return err
			}
		}
	}
	for _, query := range []string{
		"CREATE INDEX IF NOT EXISTS idx_fallback_html_sites_enabled ON fallback_html_sites(enabled, id)",
		"CREATE INDEX IF NOT EXISTS idx_fallback_html_publishes_active ON fallback_html_publishes(active, site_id, id)",
		"CREATE INDEX IF NOT EXISTS idx_fallback_html_publish_files_publish_path ON fallback_html_publish_files(publish_id, public_path)",
		"CREATE INDEX IF NOT EXISTS idx_fallback_html_redirects_site_from ON fallback_html_redirects(site_id, from_path)",
	} {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return SeedBuiltInTemplateSources(db)
}

func DropSchema(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	migrator := db.Migrator()
	for _, table := range []any{
		&Event{},
		&ExternalResource{},
		&RuntimeTarget{},
		&SelfStealDraft{},
		&TemplateSource{},
		&SafetyReport{},
		&PublishRedirect{},
		&PublishFile{},
		&Publish{},
		&Asset{},
		&Redirect{},
		&Page{},
		&Site{},
	} {
		if !migrator.HasTable(table) {
			continue
		}
		if err := migrator.DropTable(table); err != nil {
			return err
		}
	}
	for _, retired := range []string{"fallback_html_node_publications", "fallback_html_node_endpoints"} {
		if migrator.HasTable(retired) {
			if err := migrator.DropTable(retired); err != nil {
				return err
			}
		}
	}
	return nil
}
