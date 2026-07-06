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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
	"github.com/MalenkiySolovey/solovey-ui/web/publicsurface"
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
	ports, err := service.PortCandidates()
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
	if err := db.Create(&model.Inbound{
		Type:    "vless",
		Tag:     "vless-in",
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":32221}`),
	}).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
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

	ports, err := service.PortCandidates()
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
	var nodeArtifact NodeArtifactContract
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

func TestNodePublishPlanReportsArtifactContractAndStatus(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	publish, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	plan, err := service.GetNodePublishPlan(site.ID, publish.Version, "node-eu-1")
	if err != nil {
		t.Fatalf("GetNodePublishPlan: %v", err)
	}
	if plan.Schema != nodePublishPlanSchema || plan.NodeID != "node-eu-1" || plan.Version != publish.Version {
		t.Fatalf("unexpected node publish plan identity: %#v", plan)
	}
	if plan.Artifact.Sha256 == "" || plan.Artifact.SizeBytes <= 0 || !strings.HasSuffix(plan.Artifact.Filename, ".tar.gz") {
		t.Fatalf("unexpected node publish artifact ref: %#v", plan.Artifact)
	}
	if plan.Signature.Mode != nodeSignatureMode || !plan.Signature.Required || !plan.Apply.StagingRequired || !plan.Apply.RollbackOnFailure {
		t.Fatalf("unexpected node publish apply/signature contract: %#v", plan)
	}
	if len(plan.RequiredCapabilities) != 1 || plan.RequiredCapabilities[0].ID != nodeCapabilityPublicSite {
		t.Fatalf("missing node public-site capability requirement: %#v", plan.RequiredCapabilities)
	}
	if !hasNodeEndpoint(plan.Endpoints, "POST", "/public-site/apply") || !hasNodeEndpoint(plan.Endpoints, "GET", "/capabilities") {
		t.Fatalf("missing node endpoints: %#v", plan.Endpoints)
	}
	if plan.Status.Status != "not-targeted" || plan.Status.NodeID != "node-eu-1" {
		t.Fatalf("unexpected default node status: %#v", plan.Status)
	}
	row, err := newNodePublication(site.ID, publish.Version, "node-eu-1", plan.Artifact.Sha256, "nginx", "planned")
	if err != nil {
		t.Fatalf("newNodePublication: %v", err)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create node publication: %v", err)
	}
	plan, err = service.GetNodePublishPlan(site.ID, publish.Version, "node-eu-1")
	if err != nil {
		t.Fatalf("GetNodePublishPlan with status: %v", err)
	}
	if plan.Status.Status != "planned" || plan.Status.Runtime != "nginx" || plan.Status.ArtifactSha256 != plan.Artifact.Sha256 {
		t.Fatalf("stored node status was not surfaced: %#v", plan.Status)
	}
	statuses, err := service.ListNodePublications(site.ID)
	if err != nil {
		t.Fatalf("ListNodePublications: %v", err)
	}
	if len(statuses) != 1 || statuses[0].NodeID != "node-eu-1" {
		t.Fatalf("unexpected node publications: %#v", statuses)
	}
}

func TestApplyPublishToNodePushesArtifactAndStoresActiveStatus(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	publish, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	archive, err := service.GetPublishArtifact(site.ID, publish.Version)
	if err != nil {
		t.Fatalf("GetPublishArtifact: %v", err)
	}
	var validateSeen, applySeen bool
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read node request: %v", err)
		}
		if got := r.Header.Get("X-Solovey-Site-ID"); got != uintString(site.ID) {
			t.Fatalf("node site header = %q", got)
		}
		if got := r.Header.Get("X-Solovey-Runtime"); got != "gin" {
			t.Fatalf("node runtime header = %q", got)
		}
		if got := r.Header.Get("X-Solovey-Artifact-Sha256"); got != archive.Sha256 {
			t.Fatalf("node sha header = %q", got)
		}
		if got := archiveDigest(body); got != archive.Sha256 {
			t.Fatalf("node artifact body sha = %q", got)
		}
		if r.Header.Get("X-Solovey-Signature") == "" || r.Header.Get("X-Solovey-Operation-ID") == "" || r.Header.Get("X-Solovey-Nonce") == "" {
			t.Fatalf("signed operation headers were not sent: %v", r.Header)
		}
		if got := r.Header.Get("X-Solovey-Body-Sha256"); got != archive.Sha256 {
			t.Fatalf("signed body sha = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/public-site/validate":
			validateSeen = true
			_, _ = w.Write([]byte(`{"ok":true,"siteId":"` + uintString(site.ID) + `","version":"` + publish.Version + `","artifactSha256":"` + archive.Sha256 + `","files":1}`))
		case "/public-site/apply":
			applySeen = true
			_, _ = w.Write([]byte(`{"siteId":"` + uintString(site.ID) + `","version":"` + publish.Version + `","runtime":"gin","status":"applied","artifactSha256":"` + archive.Sha256 + `","appliedAt":123,"updatedAt":124}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer node.Close()
	service.nodeClient = &HTTPNodeClient{client: node.Client(), allowInsecureHTTP: true, skipURLValidation: true}

	result, err := service.ApplyPublishToNode(context.Background(), site.ID, publish.Version, NodeApplyInput{
		NodeID:       "node-local",
		BaseURL:      node.URL,
		Runtime:      "gin",
		SharedSecret: "secret",
	}, "tester")
	if err != nil {
		t.Fatalf("ApplyPublishToNode: %v", err)
	}
	if !validateSeen || !applySeen {
		t.Fatalf("node validate/apply calls = %v/%v", validateSeen, applySeen)
	}
	if result.Status.Status != "active" || result.Status.NodeID != "node-local" || result.Status.AppliedAt != 123 {
		t.Fatalf("unexpected node apply result: %#v", result)
	}
	var row fallbackdomain.NodePublication
	if err := db.Where("site_id = ? AND node_id = ? AND publish_version = ?", site.ID, "node-local", publish.Version).First(&row).Error; err != nil {
		t.Fatalf("node publication row: %v", err)
	}
	if row.Status != "active" || row.ArtifactSha256 != archive.Sha256 || row.OperationID == "" {
		t.Fatalf("unexpected stored node publication: %#v", row)
	}
	var events int64
	if err := db.Model(&fallbackdomain.Event{}).Where("site_id = ? AND action = ?", site.ID, "node_publish_applied").Count(&events).Error; err != nil {
		t.Fatalf("count node publish event: %v", err)
	}
	if events != 1 {
		t.Fatalf("node_publish_applied events = %d, want 1", events)
	}
}

func TestApplyPublishToRegisteredNodeEndpoint(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	publish, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	endpoint, err := service.SaveNodeEndpoint(NodeEndpointInput{
		NodeID:  "node-registered",
		BaseURL: "https://node.example.com",
		Runtime: "caddy",
	}, "tester")
	if err != nil {
		t.Fatalf("SaveNodeEndpoint: %v", err)
	}
	if endpoint.NodeID != "node-registered" || endpoint.Runtime != "caddy" || !endpoint.Enabled {
		t.Fatalf("unexpected node endpoint: %#v", endpoint)
	}
	service.nodeClient = nodeClientFunc{
		validate: func(_ context.Context, target NodeApplyTarget, artifact ArtifactArchive) (NodeRuntimeStatus, error) {
			if target.BaseURL != "https://node.example.com" || target.Runtime != "caddy" {
				t.Fatalf("registered target was not resolved: %#v", target)
			}
			return NodeRuntimeStatus{OK: true, SiteID: uintString(site.ID), Version: publish.Version, ArtifactSha256: artifact.Sha256}, nil
		},
		apply: func(_ context.Context, target NodeApplyTarget, artifact ArtifactArchive) (NodeRuntimeStatus, error) {
			return NodeRuntimeStatus{SiteID: uintString(site.ID), Version: publish.Version, Runtime: target.Runtime, Status: "applied", ArtifactSha256: artifact.Sha256, AppliedAt: 456}, nil
		},
	}
	result, err := service.ApplyPublishToNode(context.Background(), site.ID, publish.Version, NodeApplyInput{NodeID: "node-registered"}, "tester")
	if err != nil {
		t.Fatalf("ApplyPublishToNode: %v", err)
	}
	if result.Status.Status != "active" || result.Status.Runtime != "caddy" || result.Status.AppliedAt != 456 {
		t.Fatalf("unexpected registered apply result: %#v", result)
	}
	disabled := false
	if _, err := service.SaveNodeEndpoint(NodeEndpointInput{NodeID: "node-disabled", BaseURL: "https://disabled.example.com", Enabled: &disabled}, "tester"); err != nil {
		t.Fatalf("Save disabled endpoint: %v", err)
	}
	_, err = service.ApplyPublishToNode(context.Background(), site.ID, publish.Version, NodeApplyInput{NodeID: "node-disabled"}, "tester")
	if err == nil || !strings.Contains(err.Error(), "not registered or disabled") {
		t.Fatalf("expected disabled endpoint error, got %v", err)
	}
}

func TestApplyPublishToNodeStoresFailedStatusOnUnexpectedArtifact(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	publish, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	service.nodeClient = nodeClientFunc{
		validate: func(context.Context, NodeApplyTarget, ArtifactArchive) (NodeRuntimeStatus, error) {
			return NodeRuntimeStatus{OK: true, SiteID: uintString(site.ID), Version: publish.Version}, nil
		},
		apply: func(context.Context, NodeApplyTarget, ArtifactArchive) (NodeRuntimeStatus, error) {
			return NodeRuntimeStatus{SiteID: uintString(site.ID), Version: publish.Version, Status: "applied", ArtifactSha256: strings.Repeat("0", 64)}, nil
		},
	}
	_, err = service.ApplyPublishToNode(context.Background(), site.ID, publish.Version, NodeApplyInput{
		NodeID:  "node-bad",
		BaseURL: "https://node.example.com",
		Runtime: "gin",
	}, "tester")
	if err == nil || !strings.Contains(err.Error(), "unexpected artifact") {
		t.Fatalf("expected unexpected artifact error, got %v", err)
	}
	var row fallbackdomain.NodePublication
	if err := db.Where("site_id = ? AND node_id = ? AND publish_version = ?", site.ID, "node-bad", publish.Version).First(&row).Error; err != nil {
		t.Fatalf("node publication row: %v", err)
	}
	if row.Status != "failed" || !strings.Contains(row.LastError, "unexpected artifact") {
		t.Fatalf("unexpected failed node publication: %#v", row)
	}
}

type nodeClientFunc struct {
	validate func(context.Context, NodeApplyTarget, ArtifactArchive) (NodeRuntimeStatus, error)
	apply    func(context.Context, NodeApplyTarget, ArtifactArchive) (NodeRuntimeStatus, error)
}

func (n nodeClientFunc) Validate(ctx context.Context, target NodeApplyTarget, artifact ArtifactArchive) (NodeRuntimeStatus, error) {
	return n.validate(ctx, target, artifact)
}

func (n nodeClientFunc) Apply(ctx context.Context, target NodeApplyTarget, artifact ArtifactArchive) (NodeRuntimeStatus, error) {
	return n.apply(ctx, target, artifact)
}

func TestSelfStealDraftIsBlockedAndNeverAppliesInbound(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	publish, err := service.PublishSite(site.ID, "tester")
	if err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	oldDraft := fallbackdomain.SelfStealDraft{
		SiteID:    site.ID,
		Status:    "blocked",
		Payload:   json.RawMessage(`{"old":true}`),
		CreatedAt: time.Now().Add(-48 * time.Hour).Unix(),
	}
	if err := db.Create(&oldDraft).Error; err != nil {
		t.Fatalf("create expired self-steal draft: %v", err)
	}

	draft, err := service.CreateSelfStealDraft(site.ID, SelfStealDraftInput{}, "tester")
	if err != nil {
		t.Fatalf("CreateSelfStealDraft: %v", err)
	}
	if draft.Status != "blocked" || !draft.Payload.NoApply || draft.Payload.RequiresCapability != "inbound-draft" {
		t.Fatalf("unexpected self-steal draft: %#v", draft)
	}
	if draft.CoreDraftID == 0 || draft.Payload.CoreDraftID != draft.CoreDraftID {
		t.Fatalf("core draft id was not linked: %#v", draft)
	}
	if draft.Payload.ActivePublish != publish.Version {
		t.Fatalf("draft active publish = %q, want %q", draft.Payload.ActivePublish, publish.Version)
	}
	if containsString(draft.Payload.Blocks, "handoff is not available") {
		t.Fatalf("draft should use the core handoff instead of blocking on missing capability: %#v", draft.Payload.Blocks)
	}
	if !containsString(draft.Payload.Blocks, "self-steal requires a TLS-capable public site target") {
		t.Fatalf("draft did not block non-TLS target: %#v", draft.Payload.Blocks)
	}
	if draft.Payload.InboundType != "vless" || draft.Payload.InboundTag == "" || draft.Payload.InboundCandidate == nil {
		t.Fatalf("draft candidate was not populated: %#v", draft.Payload)
	}
	if containsString(draft.Payload.ConservativeDefaults, "scMinPostsIntervalMs") || containsString(draft.Payload.ConservativeDefaults, "scMaxEachPostBytes") {
		t.Fatalf("draft should not include DPI-triggering transport parameters: %#v", draft.Payload.ConservativeDefaults)
	}
	var stored fallbackdomain.SelfStealDraft
	if err := db.First(&stored, draft.ID).Error; err != nil {
		t.Fatalf("stored self-steal draft: %v", err)
	}
	if stored.Status != "blocked" || stored.CoreDraftID != draft.CoreDraftID || !strings.Contains(string(stored.Payload), `"noApply":true`) {
		t.Fatalf("stored draft should be blocked/no-apply: %#v", stored)
	}
	var coreDraft model.InboundDraft
	if err := db.First(&coreDraft, draft.CoreDraftID).Error; err != nil {
		t.Fatalf("core inbound draft: %v", err)
	}
	if coreDraft.Status != "blocked" || coreDraft.Source != "fallback-html:self-steal" || coreDraft.InboundType != "vless" || coreDraft.Tag != draft.Payload.InboundTag {
		t.Fatalf("unexpected core inbound draft: %#v", coreDraft)
	}
	if !strings.Contains(string(coreDraft.Payload), `"coreDraftId":`) || !strings.Contains(string(coreDraft.Payload), `"inboundCandidate"`) {
		t.Fatalf("core draft payload missing handoff details: %s", string(coreDraft.Payload))
	}
	var inboundCount int64
	if err := db.Model(&model.Inbound{}).Count(&inboundCount).Error; err != nil {
		t.Fatalf("count inbounds: %v", err)
	}
	if inboundCount != 0 {
		t.Fatalf("self-steal draft must not create live inbounds, got %d", inboundCount)
	}
	var oldDraftCount int64
	if err := db.Model(&fallbackdomain.SelfStealDraft{}).Where("id = ?", oldDraft.ID).Count(&oldDraftCount).Error; err != nil {
		t.Fatalf("count expired self-steal draft: %v", err)
	}
	if oldDraftCount != 0 {
		t.Fatalf("expired self-steal draft was not cleaned up")
	}
	var events int64
	if err := db.Model(&fallbackdomain.Event{}).Where("site_id = ? AND action = ?", site.ID, "self_steal_draft_blocked").Count(&events).Error; err != nil {
		t.Fatalf("count self-steal event: %v", err)
	}
	if events != 1 {
		t.Fatalf("self-steal events = %d, want 1", events)
	}
	if _, err := service.CreateSelfStealDraft(site.ID, SelfStealDraftInput{Profile: "xray"}, "tester"); err == nil {
		t.Fatalf("unsupported profile should be rejected")
	}
}

func TestSelfStealDraftCreatesReviewRequiredCoreDraftWhenSafe(t *testing.T) {
	db, dbDir := openFallbackDB(t)
	t.Setenv("SUI_DB_FOLDER", dbDir)
	setSetting(t, db, "webPath", "/secret-panel/")
	setSetting(t, db, "webCertFile", "/tmp/fullchain.pem")
	setSetting(t, db, "webKeyFile", "/tmp/privkey.pem")
	service := New(db, NewRuntime())
	site, err := service.SaveSite(SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}

	draft, err := service.CreateSelfStealDraft(site.ID, SelfStealDraftInput{HandshakeHost: "front.example.com"}, "tester")
	if err != nil {
		t.Fatalf("CreateSelfStealDraft: %v", err)
	}
	if draft.Status != "ready" || len(draft.Payload.Blocks) != 0 || !draft.Payload.NoApply {
		t.Fatalf("safe self-steal draft should be ready but not applied: %#v", draft)
	}
	if draft.Payload.HandshakeHost != "front.example.com" {
		t.Fatalf("handshake host = %q", draft.Payload.HandshakeHost)
	}
	var coreDraft model.InboundDraft
	if err := db.First(&coreDraft, draft.CoreDraftID).Error; err != nil {
		t.Fatalf("core inbound draft: %v", err)
	}
	if coreDraft.Status != "review_required" || coreDraft.ExpiresAt <= coreDraft.CreatedAt {
		t.Fatalf("unexpected core draft state: %#v", coreDraft)
	}
	var inboundCount int64
	if err := db.Model(&model.Inbound{}).Count(&inboundCount).Error; err != nil {
		t.Fatalf("count inbounds: %v", err)
	}
	if inboundCount != 0 {
		t.Fatalf("ready draft must still not create live inbounds, got %d", inboundCount)
	}
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

func hasNodeEndpoint(values []NodeEndpointContract, method string, path string) bool {
	for _, value := range values {
		if value.Method == method && value.Path == path {
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
			"assets":["assets/site.css"],
			"notes":["local test catalog"]
		}`))
	})
	mux.HandleFunc("/templates/remote-status-board/pages/index.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Status</title><link rel="stylesheet" href="../assets/site.css"></head><body><main><h1>Remote status board</h1><a href="/about/">About</a></main></body></html>`))
	})
	mux.HandleFunc("/templates/remote-status-board/pages/about.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>About</title><link rel="stylesheet" href="../assets/site.css"></head><body><main><h1>About status</h1><a href="https://example.com/status">Public status</a></main></body></html>`))
	})
	mux.HandleFunc("/templates/remote-status-board/pages/404.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Missing</title></head><body><main><h1>Not found</h1></main></body></html>`))
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
	if site.TemplateID != "remote-status-board" || len(site.Pages) != 3 || len(site.Assets) != 1 {
		t.Fatalf("unexpected remote-created site: %#v", site)
	}
	home := pageByPath(site.Pages, "/")
	if home == nil || home.ContentMode != fallbackdomain.ContentModeStaticHTML || !strings.Contains(home.Body, site.Assets[0].LogicalPath) || strings.Contains(home.Body, "../assets/site.css") {
		t.Fatalf("home page did not become static html with rewritten asset: %#v assets=%#v", home, site.Assets)
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
	if !strings.Contains(csp, "script-src 'none'") || !strings.Contains(csp, "connect-src 'none'") || !strings.Contains(csp, "frame-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") {
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
