//go:build !minimal

package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestPublishSiteWritesManifestFilesAndActivatesSnapshot(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}

	result, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	if result.Files < 4 {
		t.Fatalf("published files = %d, want at least 4", result.Files)
	}
	var publish fallbackdomain.Publish
	if err := db.Preload("Files").Where("site_id = ? AND active = ?", site.ID, true).First(&publish).Error; err != nil {
		t.Fatalf("active publish: %v", err)
	}
	paths := map[string]fallbackdomain.PublishFile{}
	for _, file := range publish.Files {
		paths[file.PublicPath] = file
		if _, err := os.Stat(file.FilePath); err != nil {
			t.Fatalf("published file %s missing: %v", file.FilePath, err)
		}
	}
	for _, publicPath := range []string{"/sitemap.xml", "/robots.txt", "/404.html"} {
		file, ok := paths[publicPath]
		if !ok {
			t.Fatalf("%s was not published", publicPath)
		}
		data, err := os.ReadFile(file.FilePath)
		if err != nil {
			t.Fatalf("read %s: %v", publicPath, err)
		}
		text := string(data)
		for _, forbidden := range []string{"/secret-panel/", "/api", "/apiv2", "/assets", "/sub/"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s leaks reserved path %q: %s", publicPath, forbidden, text)
			}
		}
	}
	if err := os.RemoveAll(publish.RootDir); err != nil {
		t.Fatalf("remove publish root: %v", err)
	}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if !runtime.ServePublic(ctx, publicsurface.Context{AdminBasePath: "/secret-panel/"}) {
		t.Fatalf("runtime did not serve public page")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("runtime status = %d, want 200", recorder.Code)
	}
}

func TestSafetyBlocksRootAdminPath(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/")
	service := New(db, NewRuntime())
	service.resourceSnapshot = func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} }
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	report, err := service.Safety(site.ID)
	if err != nil {
		t.Fatalf("Safety: %v", err)
	}
	if report.OK {
		t.Fatalf("safety should block root admin path")
	}
}

func TestPublishAllowsDefaultAppPathWarning(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/app/")
	setSetting(t, db, "webListen", "127.0.0.1")
	setSetting(t, db, "webPort", "2095")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Dev Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	report, err := service.Safety(site.ID)
	if err != nil {
		t.Fatalf("Safety: %v", err)
	}
	if report.OK || !containsWarning(report.Warnings, "default /app/") {
		t.Fatalf("safety should warn about default /app/ without hiding it: %#v", report)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite should allow the default /app/ warning for local/dev verification: %v", err)
	}
}

func TestSafetyBlocksLoopbackFallbackTarget(t *testing.T) {
	if fallback, reason := addressWouldFallback("240.0.0.1"); !fallback {
		t.Skipf("fallback path could not be exercised: %s", reason)
	}
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	setSetting(t, db, "webListen", "240.0.0.1")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	report, err := service.Safety(site.ID)
	if err != nil {
		t.Fatalf("Safety: %v", err)
	}
	if report.OK || !containsWarning(report.Warnings, "loopback fallback") {
		t.Fatalf("safety should block loopback fallback target: %#v", report)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err == nil {
		t.Fatalf("PublishSite should reject loopback fallback target")
	}
}

func TestTargetsUseCurrentManagedWebListener(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	setSetting(t, db, "webListen", "127.0.0.1")
	setSetting(t, db, "webPort", "24443")
	service := New(db, NewRuntime())
	service.resourceSnapshot = func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} }
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	targets, err := service.ListTargets(site.ID)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(targets))
	}
	target := targets[0]
	if target.Kind != "web-current" || target.Runtime != "gin" || target.Listen != "127.0.0.1" || target.Port != 24443 || target.Status != "available" {
		t.Fatalf("unexpected target: %#v", target)
	}
	if err := service.DeleteTarget(site.ID, target.ID, "tester"); err != nil {
		t.Fatalf("DeleteTarget: %v", err)
	}
	targets, err = service.ListTargets(site.ID)
	if err != nil {
		t.Fatalf("ListTargets after delete: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != 0 || !targets[0].Current {
		t.Fatalf("deleted target should fall back to current listener target: %#v", targets)
	}
	ports, err := service.PortCandidates(context.Background())
	if err != nil {
		t.Fatalf("PortCandidates: %v", err)
	}
	if !hasPortCandidate(ports, "web-current", "managed", 24443) || !hasPortCandidateStatus(ports, "free") {
		t.Fatalf("unexpected port candidates: %#v", ports)
	}
	if _, err := service.SaveTarget(site.ID, TargetInput{Kind: "custom", Port: 443}, "tester"); err == nil {
		t.Fatalf("custom target should be unsupported in Gin MVP")
	}
}

func TestPortCandidatesReportInboundAndExternalBlocks(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	setSetting(t, db, "webListen", "127.0.0.1")
	setSetting(t, db, "webPort", "24443")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	service.resourceSnapshot = func(context.Context) hostresources.ResourceSnapshot {
		return hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{{
			ID: "core:inbound:1", Owner: "core", Kind: "inbound", Name: "vless-in",
			InboundTag: "vless-in", Listen: "127.0.0.1", Port: 32221,
		}}}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy test port: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := db.Create(&fallbackdomain.RuntimeTarget{
		SiteID:   site.ID,
		Kind:     "web-current",
		Listen:   "127.0.0.1",
		Port:     port,
		RootPath: "/",
		Runtime:  "gin",
	}).Error; err != nil {
		t.Fatalf("create stale target: %v", err)
	}

	ports, err := service.PortCandidates(context.Background())
	if err != nil {
		t.Fatalf("PortCandidates: %v", err)
	}
	if !hasPortCandidate(ports, "inbound", "blocked-inbound", 32221) {
		t.Fatalf("blocked inbound candidate missing: %#v", ports)
	}
	if !hasPortCandidate(ports, "web-current", "blocked-external", port) {
		t.Fatalf("blocked external candidate missing: %#v", ports)
	}
}

func TestGinRuntimeRejectsSecondLiveSite(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	first, err := service.SaveSite(SiteInput{Name: "First Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite first: %v", err)
	}
	second, err := service.SaveSite(SiteInput{Name: "Second Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite second: %v", err)
	}
	if _, err := service.PublishSite(first.ID, "tester"); err != nil {
		t.Fatalf("PublishSite first: %v", err)
	}
	if _, err := service.PublishSite(second.ID, "tester"); err == nil || !strings.Contains(err.Error(), "already published") {
		t.Fatalf("PublishSite second should be blocked by active Gin owner, err=%v", err)
	}
	var active int64
	if err := db.Model(&fallbackdomain.Publish{}).Where("active = ?", true).Count(&active).Error; err != nil {
		t.Fatalf("active count: %v", err)
	}
	if active != 1 {
		t.Fatalf("active publishes = %d, want 1", active)
	}
	if err := service.UnpublishSite(first.ID, "tester"); err != nil {
		t.Fatalf("UnpublishSite first: %v", err)
	}
	if _, err := service.PublishSite(second.ID, "tester"); err != nil {
		t.Fatalf("PublishSite second after unpublish: %v", err)
	}
}

func TestRedirectsAreValidatedAndPublished(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	setSetting(t, db, "subPath", "/sub/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.SaveRedirect(site.ID, RedirectInput{FromPath: "/about/", ToPath: "/"}, "tester"); err == nil {
		t.Fatalf("redirect source should not be allowed to replace an existing page")
	}
	if _, err := service.SaveRedirect(site.ID, RedirectInput{FromPath: "/old-admin/", ToPath: "/secret-panel/"}, "tester"); err == nil {
		t.Fatalf("redirect target should not be allowed to point to admin path")
	}
	internal, err := service.SaveRedirect(site.ID, RedirectInput{FromPath: "/old", ToPath: "/about/", StatusCode: http.StatusMovedPermanently}, "tester")
	if err != nil {
		t.Fatalf("SaveRedirect internal: %v", err)
	}
	if internal.FromPath != "/old/" || internal.ToPath != "/about/" || internal.StatusCode != http.StatusMovedPermanently || internal.External {
		t.Fatalf("unexpected internal redirect: %#v", internal)
	}
	if _, err := service.SavePage(site.ID, PageInput{Path: "/old/", Title: "Old", Body: "collision"}, "tester"); err == nil {
		t.Fatalf("page path should not be allowed to replace an existing redirect")
	}
	external, err := service.SaveRedirect(site.ID, RedirectInput{FromPath: "/external/", ToPath: "https://example.com/path?q=1"}, "tester")
	if err != nil {
		t.Fatalf("SaveRedirect external: %v", err)
	}
	if !external.External || external.StatusCode != http.StatusFound {
		t.Fatalf("unexpected external redirect: %#v", external)
	}
	if _, err := service.SaveRedirect(site.ID, RedirectInput{FromPath: "/local/", ToPath: "https://127.0.0.1/"}, "tester"); err == nil {
		t.Fatalf("local external redirect should be rejected")
	}
	validPage, err := service.ValidatePath(site.ID, PathValidationInput{Path: "/docs", Kind: "page"})
	if err != nil {
		t.Fatalf("ValidatePath valid page: %v", err)
	}
	if !validPage.OK || validPage.CanonicalPath != "/docs/" {
		t.Fatalf("unexpected valid page result: %#v", validPage)
	}
	reserved, err := service.ValidatePath(site.ID, PathValidationInput{Path: "/secret-panel/", Kind: "page"})
	if err != nil {
		t.Fatalf("ValidatePath reserved: %v", err)
	}
	if reserved.OK || !reserved.ReservedConflict {
		t.Fatalf("reserved path should be blocked: %#v", reserved)
	}
	redirectConflict, err := service.ValidatePath(site.ID, PathValidationInput{Path: "/old/", Kind: "page"})
	if err != nil {
		t.Fatalf("ValidatePath redirect conflict: %v", err)
	}
	if redirectConflict.OK || !strings.Contains(redirectConflict.Message, "already used") {
		t.Fatalf("redirect conflict should be reported: %#v", redirectConflict)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	var publish fallbackdomain.Publish
	if err := db.Preload("Redirects").Where("site_id = ? AND active = ?", site.ID, true).First(&publish).Error; err != nil {
		t.Fatalf("active publish: %v", err)
	}
	if len(publish.Redirects) != 2 {
		t.Fatalf("published redirects = %d, want 2", len(publish.Redirects))
	}
	assertRedirect(t, runtime, "/old", http.StatusMovedPermanently, "/about/")
	assertRedirect(t, runtime, "/external/", http.StatusFound, "https://example.com/path?q=1")
	assertRedirect(t, runtime, "/about", http.StatusPermanentRedirect, "/about/")
}

func TestPublishArtifactAndRollback(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	first, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite first: %v", err)
	}
	var firstPublish fallbackdomain.Publish
	if err := db.Preload("Files").Where("site_id = ? AND version = ?", site.ID, first.Version).First(&firstPublish).Error; err != nil {
		t.Fatalf("first publish: %v", err)
	}
	assertPublishArtifact(t, firstPublish.RootDir, first.Version)
	archive, err := service.GetPublishArtifact(site.ID, first.Version)
	if err != nil {
		t.Fatalf("GetPublishArtifact: %v", err)
	}
	if archive.ContentType != "application/gzip" || !strings.HasSuffix(archive.Filename, ".tar.gz") {
		t.Fatalf("unexpected archive metadata: %#v", archive)
	}
	entries := readArtifactArchive(t, archive.Data)
	for _, name := range []string{"site.json", "targets.json", "routes.json", "safety-report.json", "node-artifact.json", "checksums.txt", "index.html"} {
		if len(entries[name]) == 0 {
			t.Fatalf("artifact archive missing %s, entries=%v", name, sortedKeys(entries))
		}
	}
	var nodeArtifact nodeArtifactContract
	if err := json.Unmarshal(entries["node-artifact.json"], &nodeArtifact); err != nil {
		t.Fatalf("decode node artifact: %v", err)
	}
	if nodeArtifact.Schema != nodeArtifactSchema || nodeArtifact.Signature.Mode != nodeSignatureMode || !nodeArtifact.Signature.Required || !nodeArtifact.Apply.AtomicSwap || len(nodeArtifact.RequiredCapabilities) != 1 {
		t.Fatalf("unexpected node artifact contract: %#v", nodeArtifact)
	}
	if nodeArtifact.RequiredCapabilities[0].ID != nodeCapabilityPublicSite || nodeArtifact.RequiredCapabilities[0].Version != nodeCapabilityVersion {
		t.Fatalf("unexpected node capability requirement: %#v", nodeArtifact.RequiredCapabilities)
	}
	var safetyRows int64
	if err := db.Model(&fallbackdomain.SafetyReport{}).Where("site_id = ? AND ok = ?", site.ID, true).Count(&safetyRows).Error; err != nil {
		t.Fatalf("safety report count: %v", err)
	}
	if safetyRows != 1 {
		t.Fatalf("safety rows = %d, want 1", safetyRows)
	}

	site, err = service.GetSite(site.ID)
	if err != nil {
		t.Fatalf("GetSite: %v", err)
	}
	var home fallbackdomain.Page
	for _, page := range site.Pages {
		if page.CanonicalPath == "/" {
			home = page
			break
		}
	}
	if home.ID == 0 {
		t.Fatal("home page not found")
	}
	home.Body = "Second published body"
	if _, err := service.SavePage(site.ID, PageInput{ID: home.ID, Path: home.CanonicalPath, Title: home.Title, Body: home.Body, IsHome: true}, "tester"); err != nil {
		t.Fatalf("SavePage: %v", err)
	}
	second, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite second: %v", err)
	}
	if first.Version == second.Version {
		t.Fatalf("publish versions should differ")
	}
	if body := recordPublic(t, runtime, "/").Body.String(); !strings.Contains(body, "Second published body") {
		t.Fatalf("second publish body missing: %s", body)
	}
	result, err := service.RollbackSite(site.ID, RollbackInput{Version: first.Version}, "tester")
	if err != nil {
		t.Fatalf("RollbackSite: %v", err)
	}
	if result.Version != first.Version {
		t.Fatalf("rollback version = %q, want %q", result.Version, first.Version)
	}
	if body := recordPublic(t, runtime, "/").Body.String(); strings.Contains(body, "Second published body") {
		t.Fatalf("rollback did not restore first snapshot: %s", body)
	}
	publishes, err := service.ListPublishes(site.ID)
	if err != nil {
		t.Fatalf("ListPublishes: %v", err)
	}
	active := 0
	for _, publish := range publishes {
		if publish.Active {
			active++
			if publish.Version != first.Version {
				t.Fatalf("active version = %q, want %q", publish.Version, first.Version)
			}
		}
	}
	if active != 1 {
		t.Fatalf("active publishes = %d, want 1", active)
	}
	var rollbackEvents int64
	if err := db.Model(&fallbackdomain.Event{}).Where("site_id = ? AND action = ?", site.ID, "site_rollback").Count(&rollbackEvents).Error; err != nil {
		t.Fatalf("rollback event count: %v", err)
	}
	if rollbackEvents != 1 {
		t.Fatalf("rollback events = %d, want 1", rollbackEvents)
	}
}

func TestPrunePublishesKeepsActiveAndRecentRollbackVersions(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}

	versions := make([]string, 0, 5)
	roots := make(map[string]string, 5)
	for index := 0; index < 5; index++ {
		site, err = service.GetSite(site.ID)
		if err != nil {
			t.Fatalf("GetSite: %v", err)
		}
		var home fallbackdomain.Page
		for _, page := range site.Pages {
			if page.CanonicalPath == "/" {
				home = page
				break
			}
		}
		if home.ID == 0 {
			t.Fatal("home page not found")
		}
		body := "Published body " + strings.Repeat("x", index+1)
		if _, err := service.SavePage(site.ID, PageInput{ID: home.ID, Path: home.CanonicalPath, Title: home.Title, Body: body, IsHome: true}, "tester"); err != nil {
			t.Fatalf("SavePage %d: %v", index, err)
		}
		result, err := service.PublishSite(site.ID, "tester")
		if err != nil {
			t.Fatalf("PublishSite %d: %v", index, err)
		}
		var publish fallbackdomain.Publish
		if err := db.Where("site_id = ? AND version = ?", site.ID, result.Version).First(&publish).Error; err != nil {
			t.Fatalf("publish row %d: %v", index, err)
		}
		versions = append(versions, result.Version)
		roots[result.Version] = publish.RootDir
	}

	result, err := service.PrunePublishes(site.ID, PrunePublishesInput{Keep: 1}, "tester")
	if err != nil {
		t.Fatalf("PrunePublishes: %v", err)
	}
	if result.Removed != 3 || result.Kept != 2 {
		t.Fatalf("prune result = %#v, want removed=3 kept=2", result)
	}
	publishes, err := service.ListPublishes(site.ID)
	if err != nil {
		t.Fatalf("ListPublishes: %v", err)
	}
	if len(publishes) != 2 {
		t.Fatalf("publishes after prune = %d, want 2", len(publishes))
	}
	kept := map[string]bool{}
	for _, publish := range publishes {
		kept[publish.Version] = true
		if publish.Active && publish.Version != versions[4] {
			t.Fatalf("active publish = %q, want latest %q", publish.Version, versions[4])
		}
	}
	if !kept[versions[4]] || !kept[versions[3]] {
		t.Fatalf("kept versions = %#v, want latest active and latest rollback", kept)
	}
	for _, version := range versions[:3] {
		if _, err := os.Stat(roots[version]); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("pruned root %s stat error = %v, want not exist", roots[version], err)
		}
	}
	for _, version := range versions[3:] {
		if _, err := os.Stat(roots[version]); err != nil {
			t.Fatalf("kept root %s missing: %v", roots[version], err)
		}
	}
	var events int64
	if err := db.Model(&fallbackdomain.Event{}).Where("site_id = ? AND action = ?", site.ID, "publishes_pruned").Count(&events).Error; err != nil {
		t.Fatalf("prune event count: %v", err)
	}
	if events != 1 {
		t.Fatalf("prune events = %d, want 1", events)
	}
}

func TestLegacySelfStealDecoderIsBoundedStrictAndNonActionable(t *testing.T) {
	raw := legacySelfStealPayload()
	inspection, err := DecodeLegacySelfStealPayload(raw)
	if err != nil || inspection.Classification != "RETIRED_NON_ACTIONABLE" ||
		!containsString(inspection.ReasonCodes, LegacySelfStealRetiredReason) {
		t.Fatalf("inspection=%#v err=%v", inspection, err)
	}
	encoded, err := json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-key", "/private/path", "203.0.113.10", "443", "tlsRecordId"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("inspection leaked %q: %s", forbidden, encoded)
		}
	}
	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"schema":"solovey-ui/fallback-html-self-steal-draft/v1","source":"fallback-html:self-steal","unknown":true}`),
		json.RawMessage(`{"schema":"solovey-ui/fallback-html-self-steal-draft/v1","source":"other"}`),
		json.RawMessage(`{`),
	} {
		inspection, err = DecodeLegacySelfStealPayload(invalid)
		if err == nil || inspection.Classification != "LEGACY_INVALID_NON_ACTIONABLE" {
			t.Fatalf("invalid payload became actionable: %#v err=%v", inspection, err)
		}
	}
	oversized := make(json.RawMessage, maxLegacySelfStealPayloadBytes+1)
	if _, err := DecodeLegacySelfStealPayload(oversized); err == nil {
		t.Fatal("oversized legacy payload was accepted")
	}
}

func TestLegacySelfStealReconciliationPreservesDataAndIsIdempotent(t *testing.T) {
	db, _ := openFallbackDB(t)
	now := time.Unix(2000, 0).UTC()
	valid := legacySelfStealPayload()
	historical := []fallbackdomain.SelfStealDraft{
		{SiteID: 1, CoreDraftID: 11, Status: "ready", Payload: valid, CreatedAt: 10},
		{SiteID: 1, CoreDraftID: 12, Status: "blocked", Payload: json.RawMessage(`{`), CreatedAt: 11},
		{SiteID: 1, CoreDraftID: 13, Status: LegacySelfStealRetiredStatus, Payload: valid, CreatedAt: 12},
	}
	if err := db.Create(&historical).Error; err != nil {
		t.Fatal(err)
	}
	coreDrafts := []model.InboundDraft{
		{Source: LegacySelfStealSource, SourceRef: "open", Status: "review_required", Payload: valid, ReviewNotes: json.RawMessage(`{"keep":true}`), CreatedAt: 10, UpdatedAt: 10},
		{Source: LegacySelfStealSource, SourceRef: "invalid", Status: "blocked", Payload: json.RawMessage(`{`), CreatedAt: 11, UpdatedAt: 11},
		{Source: LegacySelfStealSource, SourceRef: "terminal", Status: "applied", Payload: valid, CreatedAt: 12, UpdatedAt: 12},
		{Source: "other-component", SourceRef: "other", Status: "review_required", Payload: valid, CreatedAt: 13, UpdatedAt: 13},
	}
	if err := db.Create(&coreDrafts).Error; err != nil {
		t.Fatal(err)
	}
	site := fallbackdomain.Site{ID: 1, Name: "Historical", Enabled: true, Status: "published", CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	target := fallbackdomain.RuntimeTarget{SiteID: 1, Kind: "standalone", Listen: "127.0.0.1", Port: 443, Runtime: "gin", TLS: true}
	publish := fallbackdomain.Publish{SiteID: 1, Version: "keep", RootDir: "historical", Active: true, CreatedAt: 20}
	tls := model.Tls{Name: "keep", Server: json.RawMessage(`{"private_key":"secret-key"}`), Client: json.RawMessage(`{}`)}
	inbound := model.Inbound{Type: "trojan", Tag: "matching-looking"}
	for _, row := range []any{&target, &publish, &tls, &inbound} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	preservedTables := []any{&fallbackdomain.RuntimeTarget{}, &fallbackdomain.Publish{}, &model.Tls{}, &model.Inbound{}}
	preservedCounts := make([]int64, len(preservedTables))
	for i, table := range preservedTables {
		if err := db.Model(table).Count(&preservedCounts[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := ReconcileLegacySelfSteal(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	var retired, invalid fallbackdomain.SelfStealDraft
	if err := db.First(&retired, historical[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&invalid, historical[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if retired.Status != LegacySelfStealRetiredStatus || invalid.Status != LegacySelfStealInvalidStatus ||
		!bytes.Equal(retired.Payload, valid) || !bytes.Equal(invalid.Payload, historical[1].Payload) {
		t.Fatalf("historical rows=%#v %#v", retired, invalid)
	}
	var open, malformed, terminal, unrelated model.InboundDraft
	for id, destination := range map[uint]*model.InboundDraft{
		coreDrafts[0].Id: &open, coreDrafts[1].Id: &malformed, coreDrafts[2].Id: &terminal, coreDrafts[3].Id: &unrelated,
	} {
		if err := db.First(destination, id).Error; err != nil {
			t.Fatal(err)
		}
	}
	if open.Status != "discarded" || malformed.Status != "discarded" ||
		!bytes.Equal(open.Payload, valid) || !strings.Contains(string(open.ReviewNotes), LegacySelfStealRetiredReason) ||
		!strings.Contains(string(malformed.ReviewNotes), LegacySelfStealInvalidReason) {
		t.Fatalf("open=%#v malformed=%#v", open, malformed)
	}
	if terminal.Status != "applied" || terminal.UpdatedAt != 12 ||
		unrelated.Status != "review_required" || unrelated.UpdatedAt != 13 {
		t.Fatalf("terminal=%#v unrelated=%#v", terminal, unrelated)
	}
	snapshot := []byte(open.Status + string(open.ReviewNotes) + strconv.FormatInt(open.UpdatedAt, 10))
	if err := ReconcileLegacySelfSteal(context.Background(), db, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&open, coreDrafts[0].Id).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshot, []byte(open.Status+string(open.ReviewNotes)+strconv.FormatInt(open.UpdatedAt, 10))) {
		t.Fatalf("idempotent reconciliation rewrote the row: %#v", open)
	}
	for i, table := range preservedTables {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil || count != preservedCounts[i] {
			t.Fatalf("preserved table %T count=%d want=%d err=%v", table, count, preservedCounts[i], err)
		}
	}
}

func TestLegacySelfStealReconciliationCancellationRollsBack(t *testing.T) {
	db, _ := openFallbackDB(t)
	row := model.InboundDraft{
		Source: LegacySelfStealSource, SourceRef: "cancel", Status: "review_required",
		Payload: legacySelfStealPayload(), CreatedAt: 10, UpdatedAt: 10,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ReconcileLegacySelfSteal(ctx, db, time.Unix(2000, 0)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled migration err=%v", err)
	}
	if err := db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "review_required" || row.UpdatedAt != 10 {
		t.Fatalf("canceled reconciliation changed row: %#v", row)
	}
}

func legacySelfStealPayload() json.RawMessage {
	return json.RawMessage(`{"schema":"solovey-ui/fallback-html-self-steal-draft/v1","source":"fallback-html:self-steal","profile":"vless-reality","noApply":true,"requiresCapability":"inbound-draft","target":{"port":443},"handshakeTarget":{"path":"/private/path"},"publicListen":"203.0.113.10","publicPort":443,"tlsRecordId":9,"inboundCandidate":{"private_key":"secret-key"},"warnings":[],"blocks":[],"conservativeDefaults":[],"nextSteps":[]}`)
}

func TestImportSiteReplacesDraftContentOnly(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}

	result, err := service.ImportSite(site.ID, SiteImportInput{
		Schema: "solovey-ui/fallback-html-site/v1",
		Pages: []SiteImportPage{
			{CanonicalPath: "/", Title: "Imported Home", Body: "<script>bad()</script><p>Hello</p>", ContentMode: "html"},
			{Path: "/docs/", Title: "Docs", Body: "Read the docs."},
		},
		Redirects: []SiteImportRedirect{
			{FromPath: "/old/", ToPath: "/docs/", StatusCode: http.StatusMovedPermanently},
		},
	}, "tester")
	if err != nil {
		t.Fatalf("ImportSite: %v", err)
	}
	if result.Pages != 2 || result.Redirects != 1 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	pages, err := service.ListPages(site.ID)
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages) != 2 || pages[0].CanonicalPath != "/" || !pages[0].IsHome || strings.Contains(pages[0].Body, "<script") {
		t.Fatalf("unexpected imported pages: %#v", pages)
	}
	redirects, err := service.ListRedirects(site.ID)
	if err != nil {
		t.Fatalf("ListRedirects: %v", err)
	}
	if len(redirects) != 1 || redirects[0].FromPath != "/old/" || redirects[0].ToPath != "/docs/" {
		t.Fatalf("unexpected imported redirects: %#v", redirects)
	}
	var stored fallbackdomain.Site
	if err := db.First(&stored, site.ID).Error; err != nil {
		t.Fatalf("stored site: %v", err)
	}
	if stored.Status != "draft" {
		t.Fatalf("imported site status = %q, want draft", stored.Status)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite imported: %v", err)
	}
	if body := recordPublic(t, runtime, "/").Body.String(); !strings.Contains(body, "Imported Home") || strings.Contains(body, "<script") {
		t.Fatalf("published imported body is wrong: %s", body)
	}
}

func TestImportSiteRejectsReservedAndIncompleteRoutes(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.ImportSite(site.ID, SiteImportInput{Pages: []SiteImportPage{{Path: "/docs/", Title: "Docs"}}}, "tester"); err == nil {
		t.Fatalf("import without / should be rejected")
	}
	if _, err := service.ImportSite(site.ID, SiteImportInput{Pages: []SiteImportPage{{Path: "/secret-panel/", Title: "Bad"}}}, "tester"); err == nil {
		t.Fatalf("reserved import path should be rejected")
	}
	if _, err := service.ImportSite(site.ID, SiteImportInput{Schema: "unknown", Pages: []SiteImportPage{{Path: "/", Title: "Home"}}}, "tester"); err == nil {
		t.Fatalf("unsupported import schema should be rejected")
	}
}

func TestRestorePostOpenDeactivatesPublishes(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	oldDefaultRuntime := DefaultRuntime
	DefaultRuntime = runtime
	t.Cleanup(func() {
		DefaultRuntime = oldDefaultRuntime
		runtime.Stop()
	})
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	if status := runtime.Status(); !status.Active {
		t.Fatalf("runtime should be active after publish: %#v", status)
	}
	legacyPayload := legacySelfStealPayload()
	legacyDraft := model.InboundDraft{
		Source: LegacySelfStealSource, SourceRef: "restored", Status: "review_required",
		Payload: legacyPayload, CreatedAt: 10, UpdatedAt: 10,
	}
	if err := db.Create(&legacyDraft).Error; err != nil {
		t.Fatalf("create legacy draft: %v", err)
	}

	if err := HandleRestorePostOpen(context.Background(), db); err != nil {
		t.Fatalf("HandleRestorePostOpen: %v", err)
	}
	var activePublishes int64
	if err := db.Model(&fallbackdomain.Publish{}).Where("site_id = ? AND active = ?", site.ID, true).Count(&activePublishes).Error; err != nil {
		t.Fatalf("active publish count: %v", err)
	}
	if activePublishes != 0 {
		t.Fatalf("active publishes = %d, want 0", activePublishes)
	}
	var restored fallbackdomain.Site
	if err := db.First(&restored, site.ID).Error; err != nil {
		t.Fatalf("restored site: %v", err)
	}
	if restored.Status != "draft" || !strings.Contains(restored.LastError, "publish again") {
		t.Fatalf("unexpected restored site state: status=%q lastError=%q", restored.Status, restored.LastError)
	}
	if status := runtime.Status(); status.Active {
		t.Fatalf("runtime should be inactive after restore guard: %#v", status)
	}
	var restoreEvents int64
	if err := db.Model(&fallbackdomain.Event{}).Where("site_id = ? AND action = ?", site.ID, "site_restore_deactivated").Count(&restoreEvents).Error; err != nil {
		t.Fatalf("restore event count: %v", err)
	}
	if restoreEvents != 1 {
		t.Fatalf("restore events = %d, want 1", restoreEvents)
	}
	var retired model.InboundDraft
	if err := db.First(&retired, legacyDraft.Id).Error; err != nil {
		t.Fatalf("restored legacy draft: %v", err)
	}
	if retired.Status != "discarded" || !bytes.Equal(retired.Payload, legacyPayload) ||
		!strings.Contains(string(retired.ReviewNotes), LegacySelfStealRetiredReason) {
		t.Fatalf("legacy draft was not retired safely: %#v", retired)
	}
	firstUpdatedAt := retired.UpdatedAt
	if err := HandleRestorePostOpen(context.Background(), db); err != nil {
		t.Fatalf("second HandleRestorePostOpen: %v", err)
	}
	if err := db.First(&retired, legacyDraft.Id).Error; err != nil {
		t.Fatalf("second restored legacy draft read: %v", err)
	}
	if retired.UpdatedAt != firstUpdatedAt {
		t.Fatalf("repeated restore rewrote retired draft: updatedAt=%d want=%d", retired.UpdatedAt, firstUpdatedAt)
	}
}

func TestPublicSiteRateLimit(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	runtime.limiter = ratelimit.NewFixedWindow[string](time.Minute, 1, 8, 0)
	if status := requestPublic(t, runtime, "/"); status != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", status)
	}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if !runtime.ServePublic(ctx, publicsurface.Context{AdminBasePath: "/secret-panel/"}) {
		t.Fatalf("runtime did not handle rate-limited public request")
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatalf("rate-limited response should include Retry-After")
	}
}

func assertPublishArtifact(t *testing.T, root string, version string) {
	t.Helper()
	sitePath := filepath.Join(root, "site.json")
	data, err := os.ReadFile(sitePath)
	if err != nil {
		t.Fatalf("read site artifact: %v", err)
	}
	var artifact struct {
		Schema  string `json:"schema"`
		Version string `json:"version"`
		Files   []struct {
			PublicPath string `json:"publicPath"`
			Sha256     string `json:"sha256"`
		} `json:"files"`
		Safety SafetyReport `json:"safety"`
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("unmarshal site artifact: %v", err)
	}
	if artifact.Schema != "solovey-ui/fallback-html-site/v1" || artifact.Version != version || len(artifact.Files) == 0 || !artifact.Safety.OK {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}
	var targets struct {
		Schema  string       `json:"schema"`
		Version string       `json:"version"`
		Targets []TargetView `json:"targets"`
	}
	readArtifactJSON(t, filepath.Join(root, "targets.json"), &targets)
	if targets.Schema != "solovey-ui/fallback-html-targets/v1" || targets.Version != version || len(targets.Targets) == 0 {
		t.Fatalf("unexpected targets artifact: %#v", targets)
	}
	var routes struct {
		Schema       string            `json:"schema"`
		Version      string            `json:"version"`
		Pages        []map[string]any  `json:"pages"`
		CanonicalMap map[string]string `json:"canonicalMap"`
	}
	readArtifactJSON(t, filepath.Join(root, "routes.json"), &routes)
	if routes.Schema != "solovey-ui/fallback-html-routes/v1" || routes.Version != version || len(routes.Pages) == 0 || routes.CanonicalMap["/"] != "/" {
		t.Fatalf("unexpected routes artifact: %#v", routes)
	}
	for _, name := range []string{"targets.json", "routes.json", "safety-report.json", "checksums.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("%s missing from artifact: %v", name, err)
		}
	}
}

func readArtifactJSON(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact json %s: %v", path, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal artifact json %s: %v", path, err)
	}
}

func readArtifactArchive(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open artifact gzip: %v", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	entries := map[string][]byte{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read artifact tar: %v", err)
		}
		if header.FileInfo().IsDir() {
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read artifact entry %s: %v", header.Name, err)
		}
		entries[header.Name] = content
	}
	return entries
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func hasPortCandidate(values []PortCandidate, kind string, status string, port int) bool {
	for _, value := range values {
		if value.Kind == kind && value.Status == status && value.Port == port {
			return true
		}
	}
	return false
}

func hasPortCandidateStatus(values []PortCandidate, status string) bool {
	for _, value := range values {
		if value.Status == status {
			return true
		}
	}
	return false
}

func TestPreviewSiteRendersDraftPage(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	result, err := service.PreviewSite(site.ID, PreviewInput{Path: "/about/"}, "tester")
	if err != nil {
		t.Fatalf("PreviewSite: %v", err)
	}
	if result.Path != "/about/" {
		t.Fatalf("preview path = %q, want /about/", result.Path)
	}
	if !strings.Contains(result.HTML, "About") || strings.Contains(result.HTML, "/secret-panel/") {
		t.Fatalf("unexpected preview html: %s", result.HTML)
	}
	var previewEvents int64
	if err := db.Model(&fallbackdomain.Event{}).Where("site_id = ? AND action = ?", site.ID, "site_previewed").Count(&previewEvents).Error; err != nil {
		t.Fatalf("preview event count: %v", err)
	}
	if previewEvents != 1 {
		t.Fatalf("preview events = %d, want 1", previewEvents)
	}
}

func TestHTMLContentModeIsSanitizedBeforePublish(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	page, err := service.SavePage(site.ID, PageInput{
		Path:        "/html/",
		Title:       "HTML",
		ContentMode: "html",
		Body:        `<h2 onclick="alert(1)">Heading</h2><p>Safe <strong>body</strong><script>alert(1)</script><a href="javascript:alert(1)">bad</a></p>`,
	}, "tester")
	if err != nil {
		t.Fatalf("SavePage html: %v", err)
	}
	if page.ContentMode != "html" {
		t.Fatalf("content mode = %q, want html", page.ContentMode)
	}
	for _, forbidden := range []string{"script", "onclick", "javascript:"} {
		if strings.Contains(strings.ToLower(page.Body), forbidden) {
			t.Fatalf("stored html contains %q: %s", forbidden, page.Body)
		}
	}
	if _, err := service.SavePage(site.ID, PageInput{Path: "/bad/", Title: "Bad", ContentMode: "javascript", Body: "bad"}, "tester"); err == nil {
		t.Fatalf("unsupported content mode should be rejected")
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	body := recordPublic(t, runtime, "/html/").Body.String()
	if !strings.Contains(body, "<strong>body</strong>") {
		t.Fatalf("sanitized html was not rendered: %s", body)
	}
	for _, forbidden := range []string{"script", "onclick", "javascript:"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("published html contains %q: %s", forbidden, body)
		}
	}
}

func TestBuiltInTemplatesAreSeededAndApplied(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	var sources []fallbackdomain.TemplateSource
	if err := db.Order("template_id ASC").Find(&sources).Error; err != nil {
		t.Fatalf("template sources: %v", err)
	}
	if len(sources) != len(fallbackdomain.BuiltInTemplates()) {
		t.Fatalf("template sources = %d, want %d", len(sources), len(fallbackdomain.BuiltInTemplates()))
	}
	sourceByID := map[string]fallbackdomain.TemplateSource{}
	for _, source := range sources {
		sourceByID[source.TemplateID] = source
	}
	for _, id := range []string{"webmail-workspace", "file-vault", "status-dashboard"} {
		source, ok := sourceByID[id]
		if !ok || !strings.Contains(source.Source, "s-ui-fallback-decoys") || source.License != "MIT-adapted" {
			t.Fatalf("decoy template source %s was not seeded with provenance: %#v", id, source)
		}
	}
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Knowledge", TemplateID: "knowledge-base"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	preview, err := service.PreviewSite(site.ID, PreviewInput{}, "tester")
	if err != nil {
		t.Fatalf("PreviewSite: %v", err)
	}
	if !strings.Contains(preview.HTML, `class="layout"`) {
		t.Fatalf("knowledge-base template was not applied: %s", preview.HTML)
	}
	for _, tc := range []struct {
		id      string
		marker  string
		profile string
	}{
		{id: "webmail-workspace", marker: "Mailbox navigation", profile: "webmail"},
		{id: "file-vault", marker: "File navigation", profile: "file-cloud"},
		{id: "status-dashboard", marker: "Status navigation", profile: "dashboard"},
	} {
		site, err := service.SaveSite(SiteInput{Name: tc.id, TemplateID: tc.id}, "tester")
		if err != nil {
			t.Fatalf("SaveSite %s: %v", tc.id, err)
		}
		preview, err := service.PreviewSite(site.ID, PreviewInput{}, "tester")
		if err != nil {
			t.Fatalf("PreviewSite %s: %v", tc.id, err)
		}
		if !strings.Contains(preview.HTML, tc.marker) {
			t.Fatalf("%s template marker missing: %s", tc.id, preview.HTML)
		}
		definition, err := templateDefinitionByID(tc.id)
		if err != nil {
			t.Fatalf("templateDefinitionByID %s: %v", tc.id, err)
		}
		if definition.ContentTypeProfile != tc.profile {
			t.Fatalf("%s profile = %q, want %q", tc.id, definition.ContentTypeProfile, tc.profile)
		}
	}
}

func TestRemoteTemplateCatalogInstallCreatePublishAndDelete(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	setSetting(t, db, "webListen", "127.0.0.1")
	setSetting(t, db, "webPort", "24443")

	mux := http.NewServeMux()
	mux.HandleFunc("/templates/catalog.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schema":"solovey-ui/fallback-decoy-catalog/v1",
			"templates":[{"id":"remote-status-board","manifest":"remote-status-board/manifest.json"}]
		}`))
	})
	mux.HandleFunc("/templates/remote-status-board/manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schema":"solovey-ui/fallback-decoy-template/v1",
			"id":"remote-status-board",
			"name":"Remote Status Board",
			"license":"MIT-adapted",
			"source":{"repository":"test/fallback-pages","license":"MIT","referenceFiles":["upstreams/FallbackHTML/status"]},
			"contentTypeProfile":"dashboard",
			"pages":["pages/index.html","pages/about.html","pages/404.html"],
			"assets":["assets/site.css","assets/decoy-interactivity.js"],
			"notes":["local test catalog"]
		}`))
	})
	mux.HandleFunc("/templates/remote-status-board/pages/index.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Status</title><link rel="stylesheet" href="../assets/site.css"></head><body><main><h1>Remote status board</h1><a href="/about/">About</a></main><script src="../assets/decoy-interactivity.js"></script></body></html>`))
	})
	mux.HandleFunc("/templates/remote-status-board/pages/about.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>About</title><link rel="stylesheet" href="../assets/site.css"></head><body><main><h1>About status</h1><a href="https://example.com/status">Public status</a></main><script src="../assets/decoy-interactivity.js"></script></body></html>`))
	})
	mux.HandleFunc("/templates/remote-status-board/pages/404.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Missing</title></head><body><main><h1>Not found</h1></main><script src="../assets/decoy-interactivity.js"></script></body></html>`))
	})
	mux.HandleFunc("/templates/remote-status-board/assets/site.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write([]byte(`body{font-family:system-ui;background:#f7fafc;color:#111827}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	runtime := NewRuntime()
	service := New(db, runtime)
	service.remoteCatalogURL = server.URL + "/templates/catalog.json"
	service.templateHTTP = server.Client()

	catalog, err := service.ListRemoteTemplateCatalog(context.Background())
	if err != nil {
		t.Fatalf("ListRemoteTemplateCatalog: %v", err)
	}
	if len(catalog.Templates) != 1 || catalog.Templates[0].ID != "remote-status-board" || catalog.Templates[0].Installed {
		t.Fatalf("unexpected remote catalog before install: %#v", catalog)
	}
	installed, err := service.InstallRemoteTemplate(context.Background(), "remote-status-board", "tester")
	if err != nil {
		t.Fatalf("InstallRemoteTemplate: %v", err)
	}
	if !installed.Installed || installed.ContentTypeProfile != "dashboard" {
		t.Fatalf("unexpected installed template: %#v", installed)
	}
	catalog, err = service.ListRemoteTemplateCatalog(context.Background())
	if err != nil {
		t.Fatalf("ListRemoteTemplateCatalog after install: %v", err)
	}
	if !catalog.Templates[0].Installed {
		t.Fatalf("remote catalog did not mark template as installed: %#v", catalog)
	}

	site, err := service.CreateSiteFromTemplate("remote-status-board", "tester")
	if err != nil {
		t.Fatalf("CreateSiteFromTemplate remote: %v", err)
	}
	if site.TemplateID != "remote-status-board" || len(site.Pages) != 3 || len(site.Assets) != 2 {
		t.Fatalf("unexpected remote-created site: %#v", site)
	}
	var stylesheet fallbackdomain.Asset
	for _, asset := range site.Assets {
		if strings.HasPrefix(asset.MimeType, "text/css") {
			stylesheet = asset
			break
		}
	}
	if stylesheet.ID == 0 {
		t.Fatalf("remote-created site did not include stylesheet: %#v", site.Assets)
	}
	home := pageByPath(site.Pages, "/")
	if home == nil || home.ContentMode != fallbackdomain.ContentModeStaticHTML || !strings.Contains(home.Body, stylesheet.LogicalPath) || strings.Contains(home.Body, "../assets/site.css") {
		t.Fatalf("home page did not become static html with rewritten asset: %#v assets=%#v", home, site.Assets)
	}
	preview, err := service.PreviewSite(site.ID, PreviewInput{}, "tester")
	if err != nil {
		t.Fatalf("PreviewSite remote: %v", err)
	}
	if !strings.Contains(preview.HTML, "<style data-fallback-preview=") || strings.Contains(preview.HTML, `<link rel="stylesheet" href="`+stylesheet.LogicalPath+`"`) {
		t.Fatalf("preview did not inline stylesheet asset: %s", preview.HTML)
	}
	publish, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite remote: %v", err)
	}
	var active fallbackdomain.Publish
	if err := db.Preload("Files").Where("site_id = ? AND version = ?", site.ID, publish.Version).First(&active).Error; err != nil {
		t.Fatalf("published remote site: %v", err)
	}
	indexFile := publishFileByPath(active.Files, "/")
	if indexFile == nil {
		t.Fatalf("remote publish did not include / file: %#v", active.Files)
	}
	data, err := os.ReadFile(indexFile.FilePath)
	if err != nil {
		t.Fatalf("read published index: %v", err)
	}
	if !strings.Contains(string(data), "Remote status board") || strings.Contains(string(data), "../assets/site.css") {
		t.Fatalf("published static html was not preserved/rewritten: %s", string(data))
	}

	if err := service.DeleteRemoteTemplate("remote-status-board", "tester"); err != nil {
		t.Fatalf("DeleteRemoteTemplate: %v", err)
	}
	if _, err := service.CreateSiteFromTemplate("remote-status-board", "tester"); err == nil {
		t.Fatalf("deleted remote template should not create new sites")
	}
	if _, err := service.SaveSite(SiteInput{Name: "Existing", TemplateID: "remote-status-board"}, "tester"); err != nil {
		t.Fatalf("existing template id used by created site should remain saveable: %v", err)
	}
}

func TestRemoteTemplateURLStaysOnCatalogHost(t *testing.T) {
	if _, err := resolveRemoteTemplateURL("https://raw.githubusercontent.com/MalenkiySolovey/solovey-fallback-pages/main/templates/catalog.json", "https://example.com/template/manifest.json"); err == nil {
		t.Fatalf("absolute cross-host remote template URL should be rejected")
	}
	resolved, err := resolveRemoteTemplateURL("https://raw.githubusercontent.com/MalenkiySolovey/solovey-fallback-pages/main/templates/catalog.json", "webmail-workspace/manifest.json")
	if err != nil {
		t.Fatalf("same-host relative template URL: %v", err)
	}
	if !strings.Contains(resolved, "/templates/webmail-workspace/manifest.json") {
		t.Fatalf("unexpected resolved URL: %s", resolved)
	}
}

func TestRemoteTemplateFilesystemRestoresWhenDatabaseTransactionFails(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)

	pageMarker := "original"
	mux := http.NewServeMux()
	mux.HandleFunc("/templates/catalog.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schema":"solovey-ui/fallback-decoy-catalog/v1","templates":[{"id":"atomic-template","manifest":"atomic-template/manifest.json"}]}`))
	})
	mux.HandleFunc("/templates/atomic-template/manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schema":"solovey-ui/fallback-decoy-template/v1","id":"atomic-template","name":"Atomic Template","license":"MIT","source":{"repository":"test","license":"MIT","referenceFiles":[]},"contentTypeProfile":"dashboard","pages":["pages/index.html"],"assets":["assets/decoy-interactivity.js"],"notes":[]}`))
	})
	mux.HandleFunc("/templates/atomic-template/pages/index.html", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `<!doctype html><html><body><p>%s</p><script src="../assets/decoy-interactivity.js"></script></body></html>`, pageMarker)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	service := New(db, NewRuntime())
	service.remoteCatalogURL = server.URL + "/templates/catalog.json"
	service.templateHTTP = server.Client()
	if _, err := service.InstallRemoteTemplate(context.Background(), "atomic-template", "tester"); err != nil {
		t.Fatalf("initial InstallRemoteTemplate: %v", err)
	}
	originalPath := filepath.Join(templateRoot("atomic-template"), "pages", "index.html")
	original, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read original template: %v", err)
	}

	if err := db.Callback().Create().Before("gorm:create").Register("test:reject-remote-template-event", func(tx *gorm.DB) {
		if tx.Statement.Table == (fallbackdomain.Event{}).TableName() {
			tx.AddError(errors.New("injected event failure"))
		}
	}); err != nil {
		t.Fatalf("register failure callback: %v", err)
	}
	pageMarker = "replacement"
	if _, err := service.InstallRemoteTemplate(context.Background(), "atomic-template", "tester"); err == nil {
		t.Fatal("replacement install should fail with the injected database error")
	}
	afterInstall, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read restored template after install failure: %v", err)
	}
	if !bytes.Equal(afterInstall, original) || strings.Contains(string(afterInstall), pageMarker) {
		t.Fatalf("failed install did not restore the previous template: %s", afterInstall)
	}

	if err := service.DeleteRemoteTemplate("atomic-template", "tester"); err == nil {
		t.Fatal("delete should fail with the injected database error")
	}
	afterDelete, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read restored template after delete failure: %v", err)
	}
	if !bytes.Equal(afterDelete, original) {
		t.Fatalf("failed delete did not restore the previous template: %s", afterDelete)
	}
}

func TestRemoteTemplateMetadataIsStrictAndBounded(t *testing.T) {
	var catalog remoteCatalogFile
	if err := decodeRemoteJSON([]byte(`{"schema":"solovey-ui/fallback-decoy-catalog/v1","templates":[],"unexpected":true}`), &catalog); err == nil {
		t.Fatal("unknown remote catalog fields should be rejected")
	}
	if err := decodeRemoteJSON([]byte(`{"schema":"solovey-ui/fallback-decoy-catalog/v1","templates":[]} {}`), &catalog); err == nil {
		t.Fatal("multiple remote catalog documents should be rejected")
	}
	manifest := remoteTemplateManifest{
		Schema: "solovey-ui/fallback-decoy-template/v1",
		ID:     "unsafe/id",
		Name:   "Unsafe",
		Pages:  []string{"pages/index.html"},
		Assets: []string{"assets/decoy-interactivity.js"},
	}
	if err := validateRemoteManifest(manifest.ID, manifest); err == nil {
		t.Fatal("unsafe remote template ids should be rejected")
	}
	manifest.ID = "bounded-template"
	manifest.Pages = make([]string, maxRemoteTemplateFiles)
	for index := range manifest.Pages {
		manifest.Pages[index] = fmt.Sprintf("pages/page-%d.html", index)
	}
	if err := validateRemoteManifest(manifest.ID, manifest); err == nil {
		t.Fatal("remote templates with too many files should be rejected")
	}
}

func TestAssetsAreValidatedAndPublished(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	asset, err := service.SaveAsset(site.ID, "style.css", []byte("body{color:#111827}"), "tester")
	if err != nil {
		t.Fatalf("SaveAsset css: %v", err)
	}
	if !strings.HasPrefix(asset.LogicalPath, "/media/") || asset.MimeType != "text/css; charset=utf-8" {
		t.Fatalf("unexpected asset view: %#v", asset)
	}
	again, err := service.SaveAsset(site.ID, "style.css", []byte("body{color:#111827}"), "tester")
	if err != nil {
		t.Fatalf("SaveAsset duplicate css: %v", err)
	}
	if again.ID != asset.ID || again.LogicalPath != asset.LogicalPath {
		t.Fatalf("duplicate asset should be idempotent: first=%#v again=%#v", asset, again)
	}
	if _, err := service.SaveAsset(site.ID, "app.js", []byte("alert(1)"), "tester"); err == nil {
		t.Fatalf("javascript asset should be rejected")
	}
	for _, filename := range []string{"shell.php", "archive.zip", "secret.env", "private.key", "module.so", "service.conf"} {
		if _, err := service.SaveAsset(site.ID, filename, []byte("body{color:#111827}"), "tester"); err == nil {
			t.Fatalf("%s asset should be rejected", filename)
		}
	}
	if _, err := service.SaveAsset(site.ID, "remote.css", []byte("@import url(https://example.com/x.css);"), "tester"); err == nil {
		t.Fatalf("external css import should be rejected")
	}
	if _, err := service.SaveAsset(site.ID, "binary.css", []byte{0xff, 0x00, 0x01}, "tester"); err == nil {
		t.Fatalf("binary css should be rejected")
	}
	if _, err := service.SaveAsset(site.ID, "binary.txt", []byte("valid\x00text"), "tester"); err == nil {
		t.Fatalf("text asset containing NUL should be rejected")
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	if status := requestPublic(t, runtime, asset.LogicalPath); status != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", status)
	}
	response := recordPublic(t, runtime, asset.LogicalPath)
	if got := response.Header().Get("Cache-Control"); got != assetCachePolicy {
		t.Fatalf("asset cache policy = %q, want %q", got, assetCachePolicy)
	}
}

func TestAssetStorageEnforcesPerSiteQuota(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	oldLimit := maxSiteAssetBytes
	maxSiteAssetBytes = int64(len("body{color:#111827}") + 4)
	t.Cleanup(func() { maxSiteAssetBytes = oldLimit })
	if _, err := service.SaveAsset(site.ID, "first.css", []byte("body{color:#111827}"), "tester"); err != nil {
		t.Fatalf("SaveAsset first: %v", err)
	}
	if _, err := service.SaveAsset(site.ID, "second.css", []byte("body{}"), "tester"); err == nil {
		t.Fatalf("second asset should exceed per-site quota")
	}
}

func TestDeleteSiteRemovesRuntimeAndStorage(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.SaveAsset(site.ID, "style.css", []byte("body{color:#111827}"), "tester"); err != nil {
		t.Fatalf("SaveAsset: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	if _, err := os.Stat(assetRoot(site.ID)); err != nil {
		t.Fatalf("asset root missing before delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot(), "publishes", "site-"+uintString(site.ID))); err != nil {
		t.Fatalf("publish root missing before delete: %v", err)
	}
	if err := service.DeleteSite(site.ID, "tester"); err != nil {
		t.Fatalf("DeleteSite: %v", err)
	}
	if runtime.ServePublic(newPublicContext(t, "/"), publicsurface.Context{AdminBasePath: "/secret-panel/"}) {
		t.Fatalf("runtime should not serve deleted site")
	}
	if _, err := os.Stat(assetRoot(site.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("asset root after delete error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot(), "publishes", "site-"+uintString(site.ID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("publish root after delete error = %v, want not exist", err)
	}
}

func TestAssetStorageRejectsPreexistingSymlink(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	target := expectedAssetPath(t, site.ID, "style.css", []byte("body{color:#111827}"))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatalf("create asset dir: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.css")
	if err := os.WriteFile(outside, []byte("outside"), 0o640); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symlink unavailable in this environment: %v", err)
	}
	if _, err := service.SaveAsset(site.ID, "style.css", []byte("body{color:#111827}"), "tester"); err == nil {
		t.Fatalf("SaveAsset should reject preexisting symlink")
	}
}

func TestAssetStorageRejectsPreexistingHardlink(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	target := expectedAssetPath(t, site.ID, "style.css", []byte("body{color:#111827}"))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatalf("create asset dir: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.css")
	if err := os.WriteFile(outside, []byte("outside"), 0o640); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Link(outside, target); err != nil {
		t.Skipf("hardlink unavailable in this environment: %v", err)
	}
	if _, err := service.SaveAsset(site.ID, "style.css", []byte("body{color:#111827}"), "tester"); err == nil {
		t.Fatalf("SaveAsset should reject preexisting hardlink")
	}
}

func TestExternalResourcePolicyValidation(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	resource, err := service.SaveExternalResource(site.ID, ExternalResourceInput{Kind: "image", URL: "https://example.com/logo.png", Allowed: true}, "tester")
	if err != nil {
		t.Fatalf("SaveExternalResource: %v", err)
	}
	if resource.Kind != "image" || !resource.Allowed {
		t.Fatalf("unexpected external resource: %#v", resource)
	}
	if _, err := service.SaveExternalResource(site.ID, ExternalResourceInput{Kind: "iframe", URL: "https://example.com/"}, "tester"); err == nil {
		t.Fatalf("iframe external resource should be rejected")
	}
	for _, kind := range []string{"fetch", "websocket"} {
		if _, err := service.SaveExternalResource(site.ID, ExternalResourceInput{Kind: kind, URL: "https://example.com/"}, "tester"); err == nil {
			t.Fatalf("%s external resource should be rejected", kind)
		}
	}
	if _, err := service.SaveExternalResource(site.ID, ExternalResourceInput{Kind: "link", URL: "https://localhost/"}, "tester"); err == nil {
		t.Fatalf("local external resource should be rejected")
	}
	if _, err := service.SaveExternalResource(site.ID, ExternalResourceInput{Kind: "font", URL: "https://fonts.example.net/f.woff2", Allowed: true}, "tester"); err != nil {
		t.Fatalf("SaveExternalResource font: %v", err)
	}
	if _, err := service.SaveExternalResource(site.ID, ExternalResourceInput{Kind: "link", URL: "https://links.example.org/", Allowed: true}, "tester"); err != nil {
		t.Fatalf("SaveExternalResource link: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	response := recordPublic(t, runtime, "/")
	csp := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://example.com") || !strings.Contains(csp, "https://fonts.example.net") {
		t.Fatalf("CSP does not include passive allowlist origins: %s", csp)
	}
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "connect-src 'none'") || !strings.Contains(csp, "frame-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "form-action 'none'") {
		t.Fatalf("CSP does not keep active external content blocked by default: %s", csp)
	}
	if strings.Contains(csp, "links.example.org") {
		t.Fatalf("link-only resource should not expand CSP: %s", csp)
	}
}

func TestRuntimeStatusAndCacheValidators(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	runtime := NewRuntime()
	service := New(db, runtime)
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if status := service.RuntimeStatus(); status.Active {
		t.Fatalf("runtime should be inactive before publish: %#v", status)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	status := service.RuntimeStatus()
	if !status.Active || status.SiteID != site.ID || status.Pages < 4 {
		t.Fatalf("unexpected runtime status: %#v", status)
	}
	health := service.RuntimeHealth()
	if !health.OK || !health.Active || health.HomeStatus != http.StatusOK || health.NotFoundStatus != http.StatusNotFound || !health.AdminReserved || !health.ACMEReserved {
		t.Fatalf("unexpected runtime health: %#v", health)
	}
	response := recordPublic(t, runtime, "/")
	etag := response.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("ETag was not set")
	}
	if got := response.Header().Get("Last-Modified"); got == "" {
		t.Fatalf("Last-Modified was not set")
	}
	if got := response.Header().Get("Cache-Control"); got != htmlCachePolicy {
		t.Fatalf("html cache policy = %q, want %q", got, htmlCachePolicy)
	}
	notModified := recordPublicWithHeader(t, runtime, "/", "If-None-Match", etag)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match status = %d, want 304; etag=%q headers=%v", notModified.Code, etag, notModified.Header())
	}
	head := recordPublicMethod(t, runtime, http.MethodHead, "/")
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", head.Code)
	}
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD response body length = %d, want 0", head.Body.Len())
	}
	post := recordPublicMethod(t, runtime, http.MethodPost, "/")
	if post.Code != http.StatusOK || post.Body.Len() == 0 {
		t.Fatalf("passive POST / = %d with %d bytes, want 200 and a home page", post.Code, post.Body.Len())
	}
}

func requestPublic(t *testing.T, runtime *Runtime, path string) int {
	t.Helper()
	return recordPublic(t, runtime, path).Code
}

func recordPublic(t *testing.T, runtime *Runtime, path string) *httptest.ResponseRecorder {
	t.Helper()
	return recordPublicMethod(t, runtime, http.MethodGet, path)
}

func recordPublicWithHeader(t *testing.T, runtime *Runtime, path string, key string, value string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
	ctx.Request.Header.Set(key, value)
	if !runtime.ServePublic(ctx, publicsurface.Context{AdminBasePath: "/secret-panel/"}) {
		t.Fatalf("runtime did not serve %s", path)
	}
	return recorder
}

func recordPublicMethod(t *testing.T, runtime *Runtime, method string, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, nil)
	if !runtime.ServePublic(ctx, publicsurface.Context{AdminBasePath: "/secret-panel/"}) {
		t.Fatalf("runtime did not serve %s", path)
	}
	return recorder
}

func newPublicContext(t *testing.T, path string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return ctx
}

func assertRedirect(t *testing.T, runtime *Runtime, path string, wantStatus int, wantLocation string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
	if !runtime.ServePublic(ctx, publicsurface.Context{AdminBasePath: "/secret-panel/"}) {
		t.Fatalf("runtime did not serve redirect for %s", path)
	}
	if recorder.Code != wantStatus {
		t.Fatalf("%s status = %d, want %d", path, recorder.Code, wantStatus)
	}
	if got := recorder.Header().Get("Location"); got != wantLocation {
		t.Fatalf("%s Location = %q, want %q", path, got, wantLocation)
	}
}

func openFallbackDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	db := dbsqlite.DB()
	if err := fallbackdomain.EnsureSchema(db); err != nil {
		t.Fatalf("fallback schema: %v", err)
	}
	return db, dir
}

func setSetting(t *testing.T, db *gorm.DB, key string, value string) {
	t.Helper()
	if err := db.Where("key = ?", key).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("delete setting %s: %v", key, err)
	}
	if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
		t.Fatalf("create setting %s: %v", key, err)
	}
}

func expectedAssetPath(t *testing.T, siteID uint, filename string, data []byte) string {
	t.Helper()
	safeName, _, err := validateAssetFile(filename, data)
	if err != nil {
		t.Fatalf("validate asset file: %v", err)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	return filepath.Join(assetRoot(siteID), sha[:12]+"-"+safeName)
}

func containsWarning(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}

func pageByPath(pages []fallbackdomain.Page, publicPath string) *fallbackdomain.Page {
	for index := range pages {
		if pages[index].CanonicalPath == publicPath {
			return &pages[index]
		}
	}
	return nil
}

func publishFileByPath(files []fallbackdomain.PublishFile, publicPath string) *fallbackdomain.PublishFile {
	for index := range files {
		if files[index].PublicPath == publicPath {
			return &files[index]
		}
	}
	return nil
}
