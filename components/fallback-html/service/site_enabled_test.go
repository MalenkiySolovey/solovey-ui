//go:build !minimal

package service

import (
	"testing"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
)

func TestCreateSiteHonorsExplicitDisabledState(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	service := New(db, NewRuntime())
	disabled := false
	site, err := service.SaveSite(SiteInput{Name: "Disabled draft", Enabled: &disabled}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	var stored fallbackdomain.Site
	if err := db.First(&stored, site.ID).Error; err != nil {
		t.Fatal(err)
	}
	if site.Enabled || stored.Enabled {
		t.Fatalf("explicit disabled create became enabled: returned=%t stored=%t", site.Enabled, stored.Enabled)
	}
	var active int64
	if err := db.Model(&fallbackdomain.Publish{}).Where("site_id = ? AND active = ?", site.ID, true).Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatal("creating a disabled draft activated publication")
	}
	defaultSite, err := service.SaveSite(SiteInput{Name: "Default draft"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if !defaultSite.Enabled {
		t.Fatal("omitted enabled field lost the existing default")
	}
}
