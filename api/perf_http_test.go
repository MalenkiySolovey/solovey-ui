package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	importxuihttp "github.com/MalenkiySolovey/solovey-ui/api/importxui"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
	gormlogger "gorm.io/gorm/logger"
)

func BenchmarkAPI_Load(b *testing.B) {
	router := newAPIPerfRouter(b)
	benchmarkAPIGET(b, router, "/load", 100)
}

func BenchmarkAPI_Stats(b *testing.B) {
	router := newAPIPerfRouter(b)
	benchmarkAPIGET(b, router, "/stats?resource=user&tag=user-0000&limit=24", 100)
}

func BenchmarkAPI_Onlines(b *testing.B) {
	router := newAPIPerfRouter(b)
	benchmarkAPIGET(b, router, "/onlines", 100)
}

func BenchmarkAPI_Save(b *testing.B) {
	router := newAPIPerfRouter(b)
	payload, err := json.Marshal(map[string]string{"subTitle": "phase5"})
	if err != nil {
		b.Fatal(err)
	}
	form := url.Values{}
	form.Set("object", "settings")
	form.Set("action", "set")
	form.Set("data", string(payload))
	body := form.Encode()
	b.ReportMetric(1, "parallel_clients")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			b.Fatalf("POST /save status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func BenchmarkAPI_ImportXUIReports(b *testing.B) {
	router := newAPIPerfRouter(b)
	b.ReportMetric(float64(importxuihttp.RequestLimit), "rate_limit")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		importxuihttp.ResetRateLimits()
		for j := 0; j < importxuihttp.RequestLimit; j++ {
			req := httptest.NewRequest(http.MethodGet, "/import-xui/reports", nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				b.Fatalf("GET /import-xui/reports status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		}
	}
}

func TestAPIHTTPLoadScenariosPhase5(t *testing.T) {
	router := newAPIPerfRouter(t)
	for _, path := range []string{
		"/load",
		"/stats?resource=user&tag=user-0000&limit=24",
		"/onlines",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			statuses := runAPIPerfLoad(t, router, http.MethodGet, path, "", 100, 1000)
			if statuses[http.StatusOK] != 1000 {
				t.Fatalf("%s statuses=%v", path, statuses)
			}
			t.Logf("phase5 http load anchor: path=%s parallel=100 requests=1000 statuses=%v", path, statuses)
		})
	}
}

func TestAPIImportXUIReportsRateLimitPhase5(t *testing.T) {
	router := newAPIPerfRouter(t)
	importxuihttp.ResetRateLimits()
	statuses := runAPIPerfLoad(t, router, http.MethodGet, "/import-xui/reports", "", 1, 100)
	if statuses[http.StatusOK] != importxuihttp.RequestLimit || statuses[http.StatusTooManyRequests] != 100-importxuihttp.RequestLimit {
		t.Fatalf("unexpected rate-limit statuses=%v want ok=%d too_many=%d", statuses, importxuihttp.RequestLimit, 100-importxuihttp.RequestLimit)
	}
	t.Logf("phase5 issue36/44 anchor: GET /import-xui/reports requests=100 rate_limit=%d statuses=%v", importxuihttp.RequestLimit, statuses)
}

func benchmarkAPIGET(b *testing.B, router *gin.Engine, path string, parallelism int) {
	var failures atomic.Int64
	b.ReportMetric(float64(parallelism), "parallel_clients")
	b.SetParallelism(parallelism)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				failures.Add(1)
			}
		}
	})
	if failures.Load() != 0 {
		b.Fatalf("%s failures=%d", path, failures.Load())
	}
}

func newAPIPerfRouter(tb testing.TB) *gin.Engine {
	tb.Helper()
	initAPIPerfDB(tb)
	runtime := service.NewRuntime(nil)
	apiService := NewApiService(WithRuntime(runtime))
	configHandler := apiService.configHandler()
	telemetryHandler := apiService.telemetryHandler()
	importHandler := apiService.importXUIHandler()
	seedAPIPerfData(tb)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/load", configHandler.LoadData)
	router.GET("/stats", telemetryHandler.GetStats)
	router.GET("/onlines", telemetryHandler.GetOnlines)
	router.POST("/save", func(c *gin.Context) {
		configHandler.Save(c, "admin")
	})
	router.GET("/import-xui/reports", withTestTokenScope("admin", "admin", importHandler.ImportXuiReports))
	return router
}

func initAPIPerfDB(tb testing.TB) {
	tb.Helper()
	stopTokenUseDebouncerBeforeAPITestDBInit(tb)
	if db := dbsqlite.DB(); db != nil {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
			time.Sleep(25 * time.Millisecond)
		}
	}
	resetRateLimitState()
	importxuihttp.ResetRateLimits()
	dir := tb.TempDir()
	tb.Setenv("SUI_DB_FOLDER", dir)
	initAPITestDB(tb, filepath.Join(dir, "s-ui.db"))
	dbsqlite.DB().Config.Logger = gormlogger.Discard
	tb.Cleanup(func() {
		stopTokenUseDebouncerBeforeAPITestDBInit(tb)
		if db := dbsqlite.DB(); db != nil {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
	})
}

func seedAPIPerfData(tb testing.TB) {
	tb.Helper()
	clients := make([]model.Client, 100)
	for i := range clients {
		clients[i] = model.Client{
			Enable:      true,
			Name:        fmt.Sprintf("user-%04d", i),
			Config:      []byte(`{}`),
			Inbounds:    []byte(`[]`),
			Links:       []byte(`[]`),
			IPLimitMode: "monitor",
		}
	}
	if err := dbsqlite.DB().CreateInBatches(&clients, dbsqlite.BatchSize(dbsqlite.DB(), &model.Client{})).Error; err != nil {
		tb.Fatal(err)
	}
	now := time.Now().Unix()
	stats := make([]model.Stats, 0, 2000)
	for i := 0; i < 1000; i++ {
		ts := now - int64(i*60)
		stats = append(stats,
			model.Stats{DateTime: ts, Resource: "user", Tag: "user-0000", Direction: false, Traffic: int64(i + 1)},
			model.Stats{DateTime: ts, Resource: "user", Tag: "user-0000", Direction: true, Traffic: int64(i + 2)},
		)
	}
	if err := dbsqlite.DB().CreateInBatches(&stats, dbsqlite.BatchSize(dbsqlite.DB(), &model.Stats{})).Error; err != nil {
		tb.Fatal(err)
	}
	events := make([]model.AuditEvent, 50)
	for i := range events {
		events[i] = model.AuditEvent{
			DateTime: now - int64(i),
			Actor:    "admin",
			Event:    "xui_import",
			Resource: "database",
			Severity: service.AuditSeverityInfo,
			Details:  []byte(`{"phase":"5"}`),
		}
	}
	if err := dbsqlite.DB().CreateInBatches(&events, dbsqlite.BatchSize(dbsqlite.DB(), &model.AuditEvent{})).Error; err != nil {
		tb.Fatal(err)
	}
}

func runAPIPerfLoad(tb testing.TB, router *gin.Engine, method string, path string, body string, parallel int, requests int) map[int]int {
	tb.Helper()
	jobs := make(chan int)
	var mu sync.Mutex
	statuses := map[int]int{}
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				var reader *strings.Reader
				if body != "" {
					reader = strings.NewReader(body)
				} else {
					reader = strings.NewReader("")
				}
				req := httptest.NewRequest(method, path, reader)
				if method == http.MethodPost {
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)
				mu.Lock()
				statuses[recorder.Code]++
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < requests; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return statuses
}
