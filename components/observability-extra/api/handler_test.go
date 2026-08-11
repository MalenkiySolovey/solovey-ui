//go:build !minimal

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	observabilitysvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
	"github.com/gin-gonic/gin"
)

func TestGetObservabilityHistoryFiltersMetricBucketAndSince(t *testing.T) {
	initObservabilityAPITestDB(t)
	base := time.Now().Unix() + 100000
	observabilityService := &service.ObservabilityService{}
	if err := observabilityService.RecordObservabilitySample(observabilitysvc.ObservabilityBucket30s, observabilitysvc.ObservabilitySample{
		DateTime: base,
		CPU:      1,
		Memory:   map[string]interface{}{"current": uint64(10)},
		Network:  map[string]interface{}{"recv": uint64(100), "sent": uint64(200)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := observabilityService.RecordObservabilitySample(observabilitysvc.ObservabilityBucket30s, observabilitysvc.ObservabilitySample{
		DateTime: base + 10,
		CPU:      3,
		Memory:   map[string]interface{}{"current": uint64(30)},
		Network:  map[string]interface{}{"recv": uint64(300), "sent": uint64(400)},
	}); err != nil {
		t.Fatal(err)
	}

	router := newObservabilityAPITestRouter("observer", "observability")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/observability/history?metric=net_in&bucket=30s&since="+strconv.FormatInt(base, 10), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var msg Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success {
		t.Fatalf("observability request failed: %s", msg.Msg)
	}
	payload := msg.Obj.(map[string]interface{})
	if payload["bucket"] != "30s" || payload["metric"] != "net_in" {
		t.Fatalf("unexpected payload metadata: %#v", payload)
	}
	samples := payload["samples"].([]interface{})
	if len(samples) != 1 {
		t.Fatalf("expected one sample after since filter, got %#v", samples)
	}
	sample := samples[0].(map[string]interface{})
	if sample["dateTime"].(float64) != float64(base+10) || sample["value"].(float64) != 300 {
		t.Fatalf("unexpected metric sample: %#v", sample)
	}
}

func TestGetObservabilityHistoryRequiresObservabilityScope(t *testing.T) {
	initObservabilityAPITestDB(t)
	router := newObservabilityAPITestRouter("api-user", "read")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/observability/history", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	flushObservabilityAPIAudit(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "scope_denied").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "api-user" || event.Resource != "observability" {
		t.Fatalf("unexpected audit event: %#v", event)
	}
}

func newObservabilityAPITestRouter(actor string, scope string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(Deps{
		RequireScope: func(c *gin.Context, resource string, allowed ...string) bool {
			for _, allowedScope := range allowed {
				if scope == allowedScope {
					return true
				}
			}
			_ = (&service.AuditService{}).Record(service.AuditEvent{
				Actor:    actor,
				Event:    "scope_denied",
				Resource: resource,
				Severity: service.AuditSeverityWarn,
				Details:  map[string]any{"scope": scope, "required": allowed},
			})
			c.JSON(http.StatusForbidden, Envelope{Success: false, Msg: "insufficient scope"})
			return false
		},
		JSONObj: func(c *gin.Context, obj interface{}, err error) {
			c.JSON(http.StatusOK, Envelope{Success: err == nil, Obj: obj})
		},
		ObservabilityService: service.ObservabilityService{},
	})
	router.GET("/api/observability/history", handler.GetObservabilityHistory)
	return router
}

func initObservabilityAPITestDB(t *testing.T) {
	t.Helper()
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	testDB := dbsqlite.DB()
	t.Cleanup(func() {
		flushObservabilityAPIAudit(t)
		if testDB != nil {
			if sqlDB, err := testDB.DB(); err == nil {
				_ = sqlDB.Close()
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
}

func flushObservabilityAPIAudit(t testing.TB) {
	t.Helper()
	if err := service.StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
}
