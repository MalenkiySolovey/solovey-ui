//go:build !minimal

package importxui

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func TestImportXuiCorruptFileAuditsFailure(t *testing.T) {
	initImportXUIAPITestDB(t)
	ResetRateLimits()
	router := newImportXUIAPITestRouter("admin", "admin")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newImportXUIUploadRequest(t, "/api/import-xui", []byte("not sqlite"), "1"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("corrupt x-ui db should return 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var msg Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Success {
		t.Fatal("corrupt x-ui import should fail")
	}
	if msg.Msg != "Compatible panel database is invalid" {
		t.Fatalf("unsafe or unstable import error: %q", msg.Msg)
	}
	flushImportXUIAPIAudit(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "panel_import_failed").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "admin" || event.Resource != "database" {
		t.Fatalf("unexpected failure audit: %#v", event)
	}
}

func TestCompatiblePanelMutationsRequireExactStepUp(t *testing.T) {
	ResetRateLimits()
	var bindings [][2]string
	gateway := NewHandler(Deps{
		RequireScope: func(*gin.Context, string, ...string) bool { return true },
		RequireStepUp: func(c *gin.Context, operation, target string) bool {
			bindings = append(bindings, [2]string{operation, target})
			c.AbortWithStatusJSON(http.StatusForbidden, Envelope{Success: false, Msg: "denied"})
			return false
		},
		Audit:    func(*gin.Context, string, string, string, string, map[string]any) {},
		Actor:    func(*gin.Context) string { return "admin" },
		RemoteIP: func(*gin.Context) string { return "192.0.2.1" },
	})
	gatewayRouter := gin.New()
	gatewayRouter.POST("/api/import-xui", gateway.ImportXui)
	gatewayRouter.POST("/api/import-xui/apply", gateway.ImportXuiApply)
	gatewayRouter.POST("/api/import-xui/rollback", gateway.ImportXuiRollback)

	requests := []*http.Request{
		newImportXUIUploadRequest(t, "/api/import-xui", []byte("SQLite format 3\x00"), "0"),
		httptest.NewRequest(http.MethodPost, "/api/import-xui/apply", strings.NewReader("")),
		httptest.NewRequest(http.MethodPost, "/api/import-xui/rollback", strings.NewReader("backup=ignored")),
	}
	requests[2].Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, request := range requests {
		recorder := httptest.NewRecorder()
		gatewayRouter.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d body=%s", request.URL.Path, recorder.Code, recorder.Body.String())
		}
	}
	if len(bindings) != len(requests) {
		t.Fatalf("step-up bindings=%v, want %d", bindings, len(requests))
	}
	for _, binding := range bindings {
		if binding != [2]string{xuiStepUpOperation, xuiStepUpTarget} {
			t.Fatalf("unexpected step-up binding: %v", binding)
		}
	}
}

func newImportXUIAPITestRouter(actor string, scope string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(Deps{
		AuditService: service.AuditService{},
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
		RequireStepUp: func(*gin.Context, string, string) bool { return true },
		Audit: func(_ *gin.Context, actor string, event string, resource string, severity string, details map[string]any) {
			_ = (&service.AuditService{}).Record(service.AuditEvent{
				Actor:    actor,
				Event:    event,
				Resource: resource,
				Severity: severity,
				Details:  details,
			})
		},
		Actor:    func(*gin.Context) string { return actor },
		RemoteIP: func(*gin.Context) string { return "192.0.2.1" },
		Hostname: func(c *gin.Context) string {
			return c.Request.Host
		},
		JSONObj: func(c *gin.Context, obj interface{}, err error) {
			c.JSON(http.StatusOK, Envelope{Success: err == nil, Obj: obj})
		},
		JSONMsg: func(c *gin.Context, msg string, err error) {
			c.JSON(http.StatusOK, Envelope{Success: err == nil, Msg: msg})
		},
	})
	router.POST("/api/import-xui", handler.ImportXui)
	return router
}

func newImportXUIUploadRequest(t *testing.T, path string, content []byte, dryRun string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if dryRun != "" {
		if err := writer.WriteField("dryRun", dryRun); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.WriteField("strategy", "merge"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("db", "x-ui.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func initImportXUIAPITestDB(t *testing.T) {
	t.Helper()
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	testDB := dbsqlite.DB()
	t.Cleanup(func() {
		flushImportXUIAPIAudit(t)
		if testDB != nil {
			if sqlDB, err := testDB.DB(); err == nil {
				_ = sqlDB.Close()
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
}

func flushImportXUIAPIAudit(t testing.TB) {
	t.Helper()
	if err := service.StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
}
