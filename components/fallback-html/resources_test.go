//go:build !minimal

package fallbackhtml

import (
	"path/filepath"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPublicSiteResourceContributorPublishesOnlyActiveSites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fallback-resources.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&fallbackdomain.Site{}, &fallbackdomain.RuntimeTarget{}); err != nil {
		t.Fatal(err)
	}
	active := fallbackdomain.Site{Name: "Decoy", Enabled: true, Status: "published", Hostname: "decoy.example"}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&fallbackdomain.RuntimeTarget{SiteID: active.ID, Kind: "standalone", Listen: "127.0.0.1", Port: 8443, Runtime: "gin", TLS: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&fallbackdomain.Site{Name: "Draft", Enabled: true, Status: "draft"}).Error; err != nil {
		t.Fatal(err)
	}
	items, err := (publicSiteResourceContributor{db: db}).ListProtectableResources(t.Context())
	if err != nil || len(items) != 1 {
		t.Fatalf("ListProtectableResources: items=%#v err=%v", items, err)
	}
	item := items[0]
	if item.ID != "component:fallback-html:site:1" || item.Owner != "fallback-html" || item.Public || item.Port != 8443 || !item.TLS {
		t.Fatalf("public site resource = %#v", item)
	}
	if item.Capabilities.CanServeFallback != hostresources.CapabilityYes {
		t.Fatalf("capabilities = %#v", item.Capabilities)
	}
}
