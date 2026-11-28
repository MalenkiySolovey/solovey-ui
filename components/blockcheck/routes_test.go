//go:build !minimal

package blockcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/settings/store"
	"github.com/MalenkiySolovey/solovey-backend/storage/durableowner"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/gin-gonic/gin"
	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openCatalogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "blockcheck.db")+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBlockCheckReservesSQLiteWriterBeforeReading(t *testing.T) {
	db := openCatalogTestDB(t)
	ctx := t.Context()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	competitor, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.Close()
	if _, err := competitor.ExecContext(ctx, "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	const probe = "UPDATE settings SET value = value WHERE 0"
	err = writeStoredBlockCheck(ctx, db, errStorage, func(*gorm.DB) error {
		_, err := competitor.ExecContext(ctx, probe)
		var driver sqlite3.Error
		if !errors.As(err, &driver) || driver.Code != sqlite3.ErrBusy {
			t.Fatalf("another connection could write before the catalog read: %v", err)
		}
		return feature.ErrStaleCatalog
	})
	if !errors.Is(err, feature.ErrStaleCatalog) {
		t.Fatalf("stale revision was not preserved: %v", err)
	}
	if _, err := competitor.ExecContext(ctx, probe); err != nil {
		t.Fatalf("writer was not released after rollback: %v", err)
	}
}

func TestBlockCheckDurableCatalogRevision(t *testing.T) {
	manifest, registered := durableowner.Lookup(feature.ID)
	if !registered || !slices.Equal(manifest.Database.Tables, []string{feature.CatalogGroupTable, feature.HistoryGroupTable, feature.HistoryRunTable}) ||
		!slices.Equal(manifest.Database.Settings, []string{feature.CatalogRevisionSettingKey, feature.CatalogSettingKey, feature.HistorySettingKey}) ||
		manifest.Database.SchemaVersion != "2" || manifest.DefaultEnabled {
		t.Fatalf("incorrect ComponentHost registration: %+v", manifest)
	}
	db := openCatalogTestDB(t)
	if err := migrateStorage(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	current, err := readCatalog(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	request := feature.CatalogChange{Action: "delete", ID: "youtube", ExpectedRevision: current.Revision()}
	next, err := changeCatalog(ctx, db, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changeCatalog(ctx, db, request); !errors.Is(err, feature.ErrStaleCatalog) {
		t.Fatalf("stale transaction was not rejected: %v", err)
	}
	observed, err := readCatalog(ctx, db)
	if err != nil || observed.Revision() != next.Revision() || len(observed.Groups) != len(current.Groups)-1 {
		t.Fatalf("durable read did not match commit: %v", err)
	}
	if _, exists := observed.Group("youtube"); exists {
		t.Fatal("removed default was silently recreated")
	}
}

func TestBlockCheckMigratesLegacyCatalogWithoutReseeding(t *testing.T) {
	defaults, err := feature.DecodeCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	first, second := defaults.Groups[1], defaults.Groups[0]
	first.Name, first.Disabled = "My historical title", true
	for name, groups := range map[string][]feature.Group{"edited": {first, second}, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			db := openCatalogTestDB(t)
			legacy := feature.Catalog{Schema: feature.CatalogSchema, Groups: groups}
			data, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.UpsertValue(db, feature.CatalogSettingKey, string(data)); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := migrateStorage(t.Context(), db); err != nil {
					t.Fatal(err)
				}
			}
			got, err := readCatalog(t.Context(), db)
			if err != nil || got.Revision() != legacy.Revision() {
				t.Fatalf("legacy groups, order, or revision lost: %s != %s; %v", got.Revision(), legacy.Revision(), err)
			}
			if value, err := catalogSetting(db, feature.CatalogSettingKey); value != "" || err != nil {
				t.Fatalf("legacy row was not retired: present=%t err=%v", value != "", err)
			}
			selection, err := readRunCatalog(t.Context(), db, legacy.Revision(), "", true)
			if err != nil || len(selection.Groups) != len(groups)/2 || selection.Revision() != legacy.Revision() {
				t.Fatalf("enabled selection lost its catalog revision: %+v %v", selection, err)
			}
		})
	}
}

func TestBlockCheckRunReadsOnlySelectedGroups(t *testing.T) {
	db := openCatalogTestDB(t)
	ctx := t.Context()
	if err := migrateStorage(ctx, db); err != nil {
		t.Fatal(err)
	}
	catalog, err := readCatalog(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := changeCatalog(ctx, db, feature.CatalogChange{Action: "toggle", ID: "discord", ExpectedRevision: catalog.Revision()})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&catalogGroup{}).Where("id = ?", "discord").Update("payload", "malformed").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := readCatalog(ctx, db); !errors.Is(err, feature.ErrInvalidCatalog) {
		t.Fatalf("editing ignored the damaged row: %v", err)
	}
	if only, err := readRunCatalog(ctx, db, changed.Revision(), "youtube", false); err != nil || len(only.Groups) != 1 || only.Groups[0].ID != "youtube" {
		t.Fatalf("single-group check read unrelated data: %+v %v", only, err)
	}
	if enabled, err := readRunCatalog(ctx, db, changed.Revision(), "", true); err != nil || len(enabled.Groups) != len(changed.Groups)-1 {
		t.Fatalf("Check All read the disabled group: %+v %v", enabled, err)
	}
	if _, err := readRunCatalog(ctx, db, catalog.Revision(), "youtube", false); !errors.Is(err, feature.ErrStaleCatalog) {
		t.Fatalf("run accepted stale revision: %v", err)
	}
	if err := db.Where("key = ?", feature.CatalogRevisionSettingKey).Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := readRunCatalog(ctx, db, catalog.Revision(), "youtube", false); !errors.Is(err, feature.ErrInvalidCatalog) {
		t.Fatalf("missing revision masqueraded as defaults: %v", err)
	}
}

func largeCatalogGroup(index int) feature.Group {
	group := feature.Group{Name: fmt.Sprintf("Imported service %02d", index), Disabled: true}
	for i := range 100 {
		group.Targets = append(group.Targets, feature.Target{
			Name: fmt.Sprintf("Public test endpoint %03d", i), Host: fmt.Sprintf("endpoint-%03d.example", i),
			Role: strings.Repeat("r", 96), Probes: []feature.Probe{feature.TCP},
		})
	}
	return group
}

func TestBlockCheckAuthorizationAndBoundsPrecedeExecution(t *testing.T) {
	for _, spec := range []struct {
		method, path, scope string
		limit               int
	}{
		{"GET", "/groups", "blockcheck.groups.read", 0}, {"POST", "/groups", "blockcheck.groups.write", feature.MaxGroupChangeBytes},
		{"GET", "/options", "blockcheck.options.read", 0}, {"GET", "/history", "blockcheck.history.read", 0}, {"POST", "/run", "blockcheck.run", 8 << 10},
		{"POST", "/run/stream", "blockcheck.run", feature.MaxSuiteRequestBytes}, {"POST", "/run/cancel", "blockcheck.run", 512},
	} {
		t.Run(spec.method+spec.path, func(t *testing.T) {
			g := gin.New()
			allow := false
			registerRoutes(g.Group("/api"), componenthost.Ports{Auth: componenthost.AuthDeps{RequireScope: func(c *gin.Context, scope string, roles ...string) bool {
				if scope != spec.scope || !slices.Equal(roles, []string{"admin"}) {
					t.Fatalf("unbound authorization: %q %v", scope, roles)
				}
				if !allow {
					c.AbortWithStatus(http.StatusForbidden)
				}
				return allow
			}}, HTTP: componenthost.HTTPDeps{JSONObj: func(c *gin.Context, _ any, err error) {
				if err == nil {
					t.Fatal("invalid request could reach execution")
				}
				c.JSON(http.StatusBadRequest, gin.H{"msg": err.Error()})
			}}}, nil, localProvider{})
			req := func(body string) int {
				w := httptest.NewRecorder()
				g.ServeHTTP(w, httptest.NewRequest(spec.method, "/api/components/blockcheck"+spec.path, strings.NewReader(body)))
				return w.Code
			}
			if status := req(`{"unexpected":true}`); status != http.StatusForbidden {
				t.Fatalf("unauthorized request status %d", status)
			}
			if spec.method == http.MethodPost {
				allow = true
				for _, body := range []string{`{"unexpected":true}`, strings.Repeat(" ", spec.limit) + `{}`, `{} {}`} {
					if status := req(body); status != http.StatusBadRequest {
						t.Fatalf("invalid request status %d", status)
					}
				}
			}
		})
	}
}

func TestBlockCheckHistorySurvivesDatabaseReopenWithoutChangingCatalog(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "blockcheck.db")
	open := func() *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(filename), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	closeDB := func(db *gorm.DB) {
		t.Helper()
		conn, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db := open()
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := migrateStorage(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	ctx, now := t.Context(), time.Now().UTC()
	catalog, err := readCatalog(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		// Equal finish times also exercise the ID tiebreaker in the cursor.
		run := feature.HistoryRun{ID: fmt.Sprintf("test-run-%02d", i), StartedAt: now.Add(-time.Second), FinishedAt: now, Phase: "CANCELED",
			All: i%2 == 0, GroupIDs: []string{"voice"}, Groups: []feature.HistoryGroup{largeHistoryGroup("voice", 64<<10)}}
		if err := retainHistory(ctx, db, run); err != nil {
			t.Fatal(err)
		}
	}
	closeDB(db)
	info, err := os.Stat(filename)
	if err != nil || info.Size() <= feature.MaxHistoryBytes {
		t.Fatalf("fixture does not exceed the old history budget: %v, %v", info, err)
	}
	db = open()
	defer closeDB(db)
	got, err := readHistoryPage(ctx, db, now, historyFilter{groupID: "voice"})
	if err != nil || len(got.Runs) != 8 || got.Next == "" || got.Runs[0].ID != "test-run-11" || got.Runs[7].ID != "test-run-04" {
		t.Fatalf("wrong first history page: count=%d next=%q err=%v", len(got.Runs), got.Next, err)
	}
	older, err := readHistoryPage(ctx, db, now, historyFilter{groupID: "voice", before: got.Next})
	if err != nil || len(older.Runs) != 4 || older.Next != "" || older.Runs[0].ID != "test-run-03" ||
		older.Runs[3].ID != "test-run-00" || older.Runs[3].Phase != "CANCELED" || len(older.Runs[3].Groups) != 1 {
		t.Fatalf("wrong history continuation: count=%d next=%q err=%v", len(older.Runs), older.Next, err)
	}
	row := older.Runs[3].Groups[0].Report.Results[0]
	if row.AttemptedAddress != "" || row.Socket.Interface != "" || row.Socket.Egress != "fixture" {
		t.Fatal("history lost redaction or comparison evidence")
	}
	all, err := readHistoryPage(ctx, db, now, historyFilter{all: true})
	if err != nil || len(all.Runs) != 6 || all.Runs[0].ID != "test-run-10" || all.Runs[5].ID != "test-run-00" {
		t.Fatal("Check All page is not independent of single-group runs", err)
	}
	untouched, err := readCatalog(ctx, db)
	if err != nil || untouched.Revision() != catalog.Revision() {
		t.Fatalf("history changed catalog: %v", err)
	}
}

func largeHistoryGroup(id string, size int) feature.HistoryGroup {
	return feature.HistoryGroup{Report: feature.Report{GroupID: id, Results: []feature.Result{
		{Status: feature.OK, Reason: strings.Repeat("x", size), AttemptedAddress: "9.9.9.9", Socket: &feature.SocketEvidence{Egress: "fixture", Interface: "secret-if"}},
	}}}
}

func TestBlockCheckLargeHistoryRunHasScopedDetailAndPrunesInterruptedRuns(t *testing.T) {
	db := openCatalogTestDB(t)
	ctx, now := t.Context(), time.Now().UTC()
	if err := migrateStorage(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := beginHistory(ctx, db, "interrupted", now); err != nil {
		t.Fatal(err)
	}
	if err := writeHistoryGroup(db, "interrupted", 0, largeHistoryGroup("discard", 32)); err != nil {
		t.Fatal(err)
	}
	if err := beginHistory(ctx, db, "large", now); err != nil {
		t.Fatal(err)
	}
	ids := []string{"first", "second", "third"}
	for i, id := range ids {
		if err := writeHistoryGroup(db, "large", i, largeHistoryGroup(id, 800<<10)); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := readHistoryPage(ctx, db, now, historyFilter{})
	if err != nil || len(pending.Runs) != 0 {
		t.Fatal("unfinished history became visible", err)
	}
	if err := retainHistory(ctx, db, feature.HistoryRun{ID: "large", All: true, Phase: "COMPLETED", GroupIDs: ids,
		StartedAt: now.Add(-time.Second), FinishedAt: now}); err != nil {
		t.Fatal(err)
	}
	overview, err := readHistoryPage(ctx, db, now, historyFilter{all: true})
	if err != nil || len(overview.Runs) != 1 || overview.Runs[0].Hidden != 1 || overview.Runs[0].Omitted != 0 {
		t.Fatalf("oversize response was not identified as a view limit: %d %v", len(overview.Runs), err)
	}
	saved, err := readHistoryPage(ctx, db, now, historyFilter{groupID: "third"})
	if err != nil || len(saved.Runs) != 1 || len(saved.Runs[0].Groups) != 1 || saved.Runs[0].Hidden != 0 ||
		len(saved.Runs[0].Groups[0].Report.Results[0].Reason) != 800<<10 {
		t.Fatal("response bound lost saved group detail", err)
	}
	var remaining int64
	if err := db.Model(&historyGroupRow{}).Where("run_id = ?", "interrupted").Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatal("interrupted group rows were not pruned", remaining, err)
	}
}

func TestBlockCheckMigratesLegacyHistoryWithoutChangingCatalog(t *testing.T) {
	db := openCatalogTestDB(t)
	ctx, now := t.Context(), time.Now().UTC()
	legacy := feature.History{Version: 1, Runs: []feature.HistoryRun{{ID: "legacy-run", StartedAt: now.Add(-time.Second), FinishedAt: now,
		Phase: "CANCELED", GroupIDs: []string{"voice"}, Groups: []feature.HistoryGroup{largeHistoryGroup("voice", 20)}}}}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertValue(db, feature.HistorySettingKey, string(payload)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrateStorage(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := readHistoryPage(ctx, db, now, historyFilter{groupID: "voice"})
	if err != nil || len(saved.Runs) != 1 || saved.Runs[0].ID != "legacy-run" || saved.Runs[0].Phase != "CANCELED" ||
		len(saved.Runs[0].Groups) != 1 || saved.Runs[0].Groups[0].Report.Results[0].AttemptedAddress != "" {
		t.Fatal("legacy history migration lost or exposed results", err)
	}
	if legacy, err := catalogSetting(db, feature.HistorySettingKey); err != nil || legacy != "" {
		t.Fatal("legacy value remains after verified migration", err)
	}
	if err := dropCatalog(ctx, db); err != nil || db.Migrator().HasTable(&historyRunRow{}) || db.Migrator().HasTable(&historyGroupRow{}) {
		t.Fatal("approved data drop did not remove history tables", err)
	}
}
