package ipmonitor

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/gorm"
)

func admissionClient(t *testing.T, limit int, mode string, enabled bool) {
	t.Helper()
	seedIntegrationIPClient(t, "alice", limit, mode, enabled)
}

func admissionBarrier(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("admission barrier timed out")
	}
}

func admissionLoader(t *testing.T, loader func(string, time.Time) (allowCacheEntry, bool)) {
	t.Helper()
	original := loadPolicyForAdmission
	loadPolicyForAdmission = loader
	t.Cleanup(func() { loadPolicyForAdmission = original })
}

func TestAdmissionConcurrentColdPolicyLoad(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	Record("alice", "198.51.100.1")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	InvalidateAllCache()
	original := loadPolicyForAdmission
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var loads atomic.Int64
	admissionLoader(t, func(name string, now time.Time) (allowCacheEntry, bool) {
		loads.Add(1)
		once.Do(func() { close(started) })
		<-release
		return original(name, now)
	})
	const workers = 48
	var accepted atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ObserveAndAllow("alice", "198.51.100.2") {
				accepted.Add(1)
			}
		}()
	}
	close(start)
	admissionBarrier(t, started)
	close(release)
	wg.Wait()
	if accepted.Load() != 0 || loads.Load() != 1 {
		t.Fatalf("cold requests: accepted=%d policy loads=%d", accepted.Load(), loads.Load())
	}
}

func TestAdmissionLoadFencedByInvalidation(t *testing.T) {
	for _, operation := range []string{"client", "global", "reset", "clear", "warmup"} {
		t.Run(operation, func(t *testing.T) {
			initIPMonitorTestDB(t)
			admissionClient(t, 1, ModeMonitor, true)
			Record("alice", "198.51.100.1")
			if err := Flush(); err != nil {
				t.Fatal(err)
			}
			original := loadPolicyForAdmission
			started, release := make(chan struct{}), make(chan struct{})
			var loads atomic.Int64
			admissionLoader(t, func(name string, now time.Time) (allowCacheEntry, bool) {
				entry, ok := original(name, now)
				if loads.Add(1) == 1 {
					close(started)
					<-release
				}
				return entry, ok
			})
			result := make(chan bool, 1)
			go func() { result <- ObserveAndAllow("alice", "198.51.100.2") }()
			admissionBarrier(t, started)
			if err := dbsqlite.DB().Model(&model.Client{}).Where("name = ?", "alice").Update("ip_limit_mode", ModeEnforce).Error; err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "client":
				invalidateCache("alice")
			case "global":
				InvalidateAllCache()
			case "reset":
				ResetCaches()
			case "clear":
				if err := Clear("alice"); err != nil {
					t.Fatal(err)
				}
			case "warmup":
				if err := WarmUp(); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if <-result {
				t.Fatal("pre-invalidation monitor policy authorized a source")
			}
			entry, cached := cachedClient("alice", time.Now())
			if cached && entry.mode != ModeEnforce {
				t.Fatal("old policy was republished")
			}
			if got := ObserveAndAllow("alice", "198.51.100.2"); got != (operation == "clear") {
				t.Fatalf("next admission did not use current history/policy: allowed=%v", got)
			}
		})
	}
}

func TestAdmissionFencesPreparedCacheHit(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeMonitor, true)
	revision := policyRevision()
	if !ensureClientPolicy("alice", revision) {
		t.Fatal("initial policy did not load")
	}
	hash, _, ok := recordIPFields("198.51.100.1")
	if !ok {
		t.Fatal("source preparation failed")
	}
	invalidateCache("alice")
	if admitPrepared("alice", hash, nil, true, revision) {
		t.Fatal("prepared cache hit crossed invalidation")
	}
	pending.Lock()
	defer pending.Unlock()
	if len(pending.byClient["alice"]) != 0 {
		t.Fatal("obsolete source was reserved")
	}
}

func TestAdmissionPreparedPolicyExpires(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeMonitor, true)
	revision := policyRevision()
	if !ensureClientPolicy("alice", revision) {
		t.Fatal("initial policy did not load")
	}
	allowCache.Lock()
	entry := allowCache.byClient["alice"]
	entry.expiresAt = time.Now().Add(-time.Second)
	allowCache.byClient["alice"] = entry
	allowCache.Unlock()
	if admitPrepared("alice", "hash", nil, true, revision) {
		t.Fatal("prepared policy expired before the admission decision")
	}
}

func TestAdmissionWarmUpCannotPublishAcrossInvalidation(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	db := dbsqlite.DB()
	const callback = "test-ip-warmup-barrier"
	if err := db.Callback().Row().After("gorm:row").Register(callback, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "LEFT JOIN client_ips") {
			once.Do(func() { close(started) })
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Row().Remove(callback) })
	result := make(chan error, 1)
	go func() { result <- WarmUp() }()
	admissionBarrier(t, started)
	InvalidateAllCache()
	close(release)
	if <-result == nil {
		t.Fatal("warmup crossed the invalidation revision")
	}
	if _, ok := cachedClient("alice", time.Now()); ok {
		t.Fatal("obsolete warmup policy was published")
	}
}

func TestAdmissionSourceHashFailure(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	if err := dbsqlite.DB().Migrator().DropTable(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if ObserveAndAllow("alice", "198.51.100.1") || Allow("alice", "198.51.100.1") {
		t.Fatal("source hashing failure bypassed cold enforcement")
	}
}

func TestAdmissionRawDisplayOptIn(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	if err := dbsqlite.DB().Create(&model.Setting{Key: "ipShowRaw", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	if !ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("valid source rejected")
	}
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	var row model.ClientIP
	if err := dbsqlite.DB().Where("client_name = ?", "alice").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.IP != "" || row.IPDisplay == nil || *row.IPDisplay != "198.51.100.1" || !looksLikeSHA256Hex(row.IPHash) {
		t.Fatal("atomic admission changed opt-in display storage")
	}
}

func TestAdmissionPolicyChangePreservesReservations(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	if !ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("first source rejected")
	}
	if err := dbsqlite.DB().Model(&model.Client{}).Where("name = ?", "alice").Update("ip_limit_mode", ModeMonitor).Error; err != nil {
		t.Fatal(err)
	}
	InvalidateAllCache()
	if !ObserveAndAllow("alice", "198.51.100.2") {
		t.Fatal("new monitor policy not used")
	}
	if err := dbsqlite.DB().Model(&model.Client{}).Where("name = ?", "alice").Update("ip_limit_mode", ModeEnforce).Error; err != nil {
		t.Fatal(err)
	}
	InvalidateAllCache()
	if ObserveAndAllow("alice", "198.51.100.3") {
		t.Fatal("policy mutation lost accepted reservations")
	}
	if !ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("known source reuse rejected after lowering effective limit")
	}
}

func TestAdmissionLoaderFailureAndRecovery(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	original := loadPolicyForAdmission
	admissionLoader(t, func(string, time.Time) (allowCacheEntry, bool) { return allowCacheEntry{}, false })
	if ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("cold failure allowed source")
	}
	loadPolicyForAdmission = original
	if !ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("loader recovery did not restore admission")
	}
	allowCache.Lock()
	entry := allowCache.byClient["alice"]
	entry.expiresAt = time.Now().Add(-time.Second)
	allowCache.byClient["alice"] = entry
	allowCache.Unlock()
	loadPolicyForAdmission = func(string, time.Time) (allowCacheEntry, bool) { return allowCacheEntry{}, false }
	if ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("expired policy used after failed refresh")
	}
	InvalidateAllCache()
	if ObserveAndAllow("alice", "198.51.100.2") {
		t.Fatal("stale policy crossed invalidation")
	}
	loadPolicyForAdmission = original
	if ObserveAndAllow("missing-client", "198.51.100.1") {
		t.Fatal("unknown client policy allowed source")
	}
	if err := dbsqlite.DB().Migrator().DropTable(&model.ClientIP{}); err != nil {
		t.Fatal(err)
	}
	InvalidateAllCache()
	if ObserveAndAllow("alice", "198.51.100.2") {
		t.Fatal("history read failure allowed source")
	}
}

func TestAdmissionSourceAndNonEnforcingSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		limit      int
		enabled    bool
	}{
		{"monitor", ModeMonitor, 1, true}, {"disabled", ModeEnforce, 1, false},
		{"zero", ModeEnforce, 0, true}, {"negative", ModeEnforce, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initIPMonitorTestDB(t)
			admissionClient(t, tc.limit, tc.mode, tc.enabled)
			var rejected int
			cleanup, err := RegisterSecurityEventAuditHook(func(string, string, map[string]any) { rejected++ })
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			for i := 1; i <= 3; i++ {
				if !ObserveAndAllow("alice", fmt.Sprintf("198.51.100.%d", i)) {
					t.Fatal("non-enforcing policy rejected source")
				}
			}
			if rejected != 0 {
				t.Fatal("non-enforcing acceptance emitted enforcement event")
			}
			if ObserveAndAllow("alice", "not-an-ip") {
				t.Fatal("invalid nonempty source bypassed admission")
			}
			if !ObserveAndAllow("", "not-an-ip") || !ObserveAndAllow("alice", "") {
				t.Fatal("empty tracker identity contract changed")
			}
			if err := Flush(); err != nil {
				t.Fatal(err)
			}
			rows, err := History("alice", 10)
			if err != nil || len(rows) != 3 {
				t.Fatalf("non-enforcing observations lost: rows=%d err=%v", len(rows), err)
			}
		})
	}
}

func TestAdmissionAtomicDistinctSourceSlots(t *testing.T) {
	for _, tc := range []struct{ limit, consumed, remaining int }{{1, 0, 1}, {4, 0, 4}, {4, 3, 1}, {4, 4, 0}} {
		t.Run(fmt.Sprintf("limit%d-used%d", tc.limit, tc.consumed), func(t *testing.T) {
			initIPMonitorTestDB(t)
			admissionClient(t, tc.limit, ModeEnforce, true)
			for i := 1; i <= tc.consumed; i++ {
				Record("alice", fmt.Sprintf("203.0.113.%d", i))
			}
			if err := Flush(); err != nil {
				t.Fatal(err)
			}
			const workers = 64
			start := make(chan struct{})
			var accepted atomic.Int64
			var wg sync.WaitGroup
			for i := 1; i <= workers; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					if ObserveAndAllow("alice", fmt.Sprintf("198.51.100.%d", i)) {
						accepted.Add(1)
					}
				}(i)
			}
			close(start)
			wg.Wait()
			if got := accepted.Load(); got != int64(tc.remaining) {
				t.Fatalf("admitted %d distinct sources with %d slots", got, tc.remaining)
			}
			pending.Lock()
			count := len(pending.byClient["alice"])
			pending.Unlock()
			if count != tc.remaining {
				t.Fatalf("pending=%d, admitted=%d", count, accepted.Load())
			}
			if err := Flush(); err != nil {
				t.Fatal(err)
			}
			rows, err := History("alice", 100)
			if err != nil || len(rows) != tc.limit {
				t.Fatalf("accepted/rejected history: count=%d err=%v", len(rows), err)
			}
		})
	}
}

func TestAdmissionConcurrentSameSourcePrivacyAndEvent(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	var wg sync.WaitGroup
	var accepted atomic.Int64
	start := make(chan struct{})
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ObserveAndAllow("alice", "198.51.100.1") {
				accepted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 64 {
		t.Fatalf("same-source admissions=%d", accepted.Load())
	}
	var events []map[string]any
	cleanup, err := RegisterSecurityEventAuditHook(func(_ string, _ string, payload map[string]any) { events = append(events, payload) })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for i := 0; i < 3; i++ {
		if ObserveAndAllow("alice", "198.51.100.2") {
			t.Fatal("rejected source admitted")
		}
	}
	if len(events) != 1 {
		t.Fatalf("logical rejection events=%d", len(events))
	}
	payload, _ := json.Marshal(events[0])
	if strings.Contains(string(payload), "198.51.100.") || events[0]["kind"] != "ip_enforced_reject" || events[0]["client"] != "alice" || events[0]["limit"] != 1 || events[0]["count"] != 2 {
		t.Fatalf("event contract changed: %s", payload)
	}
	if hash, _ := hashIP("198.51.100.2"); events[0]["ipHash"] != hash {
		t.Fatal("event source does not use privacy owner hash")
	}
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	if !ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("persisted source reuse rejected")
	}
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	var rows []model.ClientIP
	if err := dbsqlite.DB().Where("client_name = ?", "alice").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].IP != "" || rows[0].IPDisplay != nil || !looksLikeSHA256Hex(rows[0].IPHash) {
		t.Fatalf("accepted storage privacy/uniqueness changed: rows=%d", len(rows))
	}
	history, err := History("alice", 10)
	if err != nil || len(history) != 1 || !strings.HasPrefix(history[0].IP, "masked:") {
		t.Fatal("masked history changed")
	}
}

func TestAdmissionReservationVisibleDuringFlush(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	if !ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("first source rejected")
	}
	batch := BeginFlush()
	InvalidateAllCache()
	if ObserveAndAllow("alice", "198.51.100.2") {
		t.Fatal("detached observation lost on invalidation")
	}
	if !ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("detached source reuse rejected")
	}
	batch.Requeue()
	if ObserveAndAllow("alice", "198.51.100.2") {
		t.Fatal("rollback/requeue lost reservation")
	}
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	InvalidateAllCache()
	if ObserveAndAllow("alice", "198.51.100.2") {
		t.Fatal("committed reservation lost")
	}
	rows, err := History("alice", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("flush duplicated source: count=%d err=%v", len(rows), err)
	}
}

func TestAdmissionPrecommitLoaderCannotEraseReservation(t *testing.T) {
	initIPMonitorTestDB(t)
	admissionClient(t, 1, ModeEnforce, true)
	if !ObserveAndAllow("alice", "198.51.100.1") {
		t.Fatal("first source rejected")
	}
	batch := BeginFlush()
	InvalidateAllCache()
	original := loadPolicyForAdmission
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	admissionLoader(t, func(name string, now time.Time) (allowCacheEntry, bool) {
		entry, ok := original(name, now)
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return entry, ok
	})
	result := make(chan bool, 1)
	go func() { result <- ObserveAndAllow("alice", "198.51.100.2") }()
	admissionBarrier(t, started)
	tx := dbsqlite.DB().Begin()
	if err := batch.WriteTo(tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	batch.Commit()
	close(release)
	if <-result {
		t.Fatal("precommit loader erased accepted source")
	}
	if ObserveAndAllow("alice", "198.51.100.2") {
		t.Fatal("next admission lost committed source")
	}
}

func TestAdmissionResetFencesOldFlushCompletion(t *testing.T) {
	for _, action := range []string{"commit", "requeue"} {
		t.Run(action, func(t *testing.T) {
			initIPMonitorTestDB(t)
			admissionClient(t, 1, ModeEnforce, true)
			if !ObserveAndAllow("alice", "198.51.100.1") {
				t.Fatal("first source rejected")
			}
			batch := BeginFlush()
			ResetCaches()
			if action == "commit" {
				batch.Commit()
			} else {
				batch.Requeue()
			}
			if !ObserveAndAllow("alice", "198.51.100.2") {
				t.Fatal("old batch revived reset state")
			}
		})
	}
}
