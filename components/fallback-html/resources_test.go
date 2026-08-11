//go:build !minimal

package fallbackhtml

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestFallbackTargetProviderUsesExactDigestWithoutFilesystemPathSelection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fallback-targets.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.AutoMigrate(&fallbackdomain.Site{}, &fallbackdomain.RuntimeTarget{}, &fallbackdomain.Publish{}, &fallbackdomain.PublishFile{}); err != nil {
		t.Fatal(err)
	}
	site := fallbackdomain.Site{Name: "Decoy", Enabled: true, Status: "published"}
	if err = db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	target := fallbackdomain.RuntimeTarget{SiteID: site.ID, Kind: "standalone", Listen: "127.0.0.1", Port: 8080, Runtime: "gin"}
	if err = db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	publish := fallbackdomain.Publish{SiteID: site.ID, Version: "publish-1", RootDir: "/secret/root", Active: true, CreatedAt: 1}
	if err = db.Create(&publish).Error; err != nil {
		t.Fatal(err)
	}
	file := fallbackdomain.PublishFile{PublishID: publish.ID, PublicPath: "/index.html", FilePath: "/secret/root/index.html", Sha256: strings.Repeat("a", 64), SizeBytes: 10}
	if err = db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	provider := targetProvider{db: db, now: func() time.Time { return time.Unix(100, 0) }}
	first, err := provider.ListTargets(context.Background())
	if err != nil || len(first) != 1 {
		t.Fatalf("targets=%#v err=%v", first, err)
	}
	if first[0].Identity.TargetID != "site:1" || first[0].PublishRevision != "publish-1" || len(first[0].ContentDigest) != 64 || !first[0].Endpoint.Local {
		t.Fatalf("target=%#v", first[0])
	}
	payload, _ := json.Marshal(first[0])
	if strings.Contains(string(payload), "/secret/") {
		t.Fatalf("target leaked filesystem path: %s", payload)
	}
	if err = db.Model(&fallbackdomain.Publish{}).Where("id = ?", publish.ID).Update("root_dir", "/different/root").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&fallbackdomain.PublishFile{}).Where("id = ?", file.ID).Update("file_path", "/different/root/index.html").Error; err != nil {
		t.Fatal(err)
	}
	second, err := provider.ListTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second[0].ContentDigest != first[0].ContentDigest {
		t.Fatal("filesystem path altered content digest")
	}
}

func TestFallbackTargetProviderFailsClosedOnAmbiguousTargetOrPublish(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fallback-ambiguous.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.AutoMigrate(&fallbackdomain.Site{}, &fallbackdomain.RuntimeTarget{}, &fallbackdomain.Publish{}, &fallbackdomain.PublishFile{}); err != nil {
		t.Fatal(err)
	}
	site := fallbackdomain.Site{Name: "Ambiguous", Enabled: true, Status: "published"}
	if err = db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{8080, 8081} {
		if err = db.Create(&fallbackdomain.RuntimeTarget{SiteID: site.ID, Kind: "standalone", Listen: "127.0.0.1", Port: port, Runtime: "gin"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, version := range []string{"publish-1", "publish-2"} {
		publish := fallbackdomain.Publish{SiteID: site.ID, Version: version, Active: true, CreatedAt: 1}
		if err = db.Create(&publish).Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Create(&fallbackdomain.PublishFile{PublishID: publish.ID, PublicPath: "/index.html", Sha256: strings.Repeat("a", 64), SizeBytes: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	items, err := (targetProvider{db: db, now: func() time.Time { return time.Unix(100, 0) }}).ListTargets(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("targets=%#v err=%v", items, err)
	}
	if items[0].Readiness != "UNKNOWN" || items[0].ConfidenceBP != 0 || !strings.Contains(strings.Join(items[0].ReasonCodes, ","), "ambiguous") {
		t.Fatalf("ambiguous target was presented as current: %#v", items[0])
	}
}
