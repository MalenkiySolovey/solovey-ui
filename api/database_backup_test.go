package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/MalenkiySolovey/solovey-ui/service"
	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestImportDbRequiresAdminScopeAndAuditsFailure(t *testing.T) {
	settingService := initSessionTestDB(t)

	readRouter, readCookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.POST("/api/importdb", withTestTokenScope("reader", "read", databaseImportTestHandler()))
	})
	readRecorder := performAuthenticatedTestRequest(readRouter, newDatabaseImportRequest(t, []byte("not sqlite")), readCookies...)
	if readRecorder.Code != http.StatusForbidden {
		t.Fatalf("read scope should be forbidden, got %d", readRecorder.Code)
	}

	adminRouter, adminCookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.POST("/api/importdb", withTestTokenScope("admin", "admin", databaseImportTestHandler()))
	})
	adminRecorder := performAuthenticatedTestRequest(adminRouter, newDatabaseImportRequest(t, []byte("not sqlite")), adminCookies...)
	if adminRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", adminRecorder.Code)
	}
	var msg Msg
	if err := json.Unmarshal(adminRecorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Success {
		t.Fatal("invalid db import should fail")
	}

	flushAPIAudit(t)

	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "db_import_failed").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "admin" || event.Resource != "database" || !strings.Contains(string(event.Details), `"reason":"invalid_db"`) {
		t.Fatalf("unexpected audit event: %#v details=%s", event, string(event.Details))
	}
}

func databaseImportTestHandler() gin.HandlerFunc {
	handler := (&ApiService{}).dbTransferHandler()
	handler.AuthorizationChanged = func() {}
	handler.RequireStepUp = func(*gin.Context, string, string) bool { return true }
	manager := datalifecycle.NewManager()
	manager.Admit = func(string) bool { return true }
	handler.DataLifecycle = manager
	return handler.ImportDb
}

func TestRestoreAPIProjectsAcceptedOwnerChangeAndIdempotentReplay(t *testing.T) {
	settings := initSessionTestDB(t)
	data, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	handler := (&ApiService{}).dbTransferHandler()
	accepted := 0
	handler.AuthorizationChanged = func() { accepted++ }
	handler.RequireStepUp = func(*gin.Context, string, string) bool { return true }
	manager := datalifecycle.NewManager()
	manager.Admit = func(string) bool { return true }
	handler.DataLifecycle = manager
	router, cookies := newAuthenticatedTestRouter(t, settings, func(router *gin.Engine) {
		router.POST("/api/importdb", withTestTokenScope("admin", "admin", handler.ImportDb))
	})
	var first operationcoordination.Change
	for attempt := range 2 {
		recorder := performAuthenticatedTestRequest(router, newDatabaseImportRequest(t, data), cookies...)
		var response struct {
			Success bool `json:"success"`
			Object  struct {
				Operation datalifecycle.OperationView `json:"operation"`
			} `json:"obj"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || !response.Success {
			t.Fatalf("restore attempt %d status=%d response=%s err=%v", attempt, recorder.Code, recorder.Body.String(), err)
		}
		view := response.Object.Operation
		if view.State != "APPLIED" || view.Change.Outcome != operationcoordination.OutcomeCommitted ||
			view.Change.CheckpointRef == "" || view.Change.RecoveryRequired || view.Change.RollbackVerified {
			t.Fatalf("restore acceptance projected false truth: %+v", view.Change)
		}
		if attempt == 0 {
			first = view.Change
		} else if first != view.Change {
			t.Fatalf("idempotent replay changed correlation/revision: %+v %+v", first, view.Change)
		}
		if strings.Contains(recorder.Body.String(), "idempotencyKey") {
			t.Fatal("private idempotency authority was exposed")
		}
	}
	if accepted != 1 {
		t.Fatalf("restore accepted %d times", accepted)
	}
	persisted, err := manager.Operation(t.Context(), first.OperationID)
	if err != nil || datalifecycle.ProjectChange(persisted) != first {
		t.Fatalf("reconnect projection differs from durable owner: %+v %v", persisted, err)
	}
}

func TestDownloadDatabaseAuditsExport(t *testing.T) {
	settingService := initSessionTestDB(t)
	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.GET("/api/getdb", withTestTokenScope("admin", "admin", (&ApiService{}).dbTransferHandler().DownloadDatabase))
	})
	recorder := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, "/api/getdb", nil), cookies...)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("empty database export")
	}
	flushAPIAudit(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "db_exported").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "admin" || event.Resource != "database" || !strings.Contains(string(event.Details), `"channel":"download"`) {
		t.Fatalf("unexpected audit event: %#v details=%s", event, string(event.Details))
	}
}

func TestDownloadDatabaseEncryptedWithFixtureBackupPassphrase(t *testing.T) {
	settingService := initSessionTestDB(t)
	passphrase := "correct horse battery staple"
	saveFixtureBackupPassphrase(t, settingService, passphrase)
	registerFixtureBackupTransferCodecsForTest(t, settingService)
	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.GET("/api/getdb", withTestTokenScope("admin", "admin", (&ApiService{}).dbTransferHandler().DownloadDatabase))
	})
	recorder := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, "/api/getdb?backupEncryption=test-fixture-codec&exclude=stats,audit,unknown", nil), cookies...)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	envelope := recorder.Body.Bytes()
	if !backupenvelope.IsEnvelope(envelope) {
		t.Fatalf("encrypted backup did not return fixture backup envelope")
	}
	plaintext, err := backupenvelope.Open(envelope, []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	isDB, err := backup.IsSQLite(bytes.NewReader(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if !isDB {
		t.Fatal("decrypted encrypted backup is not SQLite")
	}

	flushAPIAudit(t)

	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "fixture_backup_manual_encrypted").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	var details map[string]any
	if err := json.Unmarshal(event.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["channel"] != "local_download" {
		t.Fatalf("unexpected audit details: %#v", details)
	}
	excluded, ok := details["excludedTables"].([]any)
	if !ok || len(excluded) != 2 || excluded[0] != "stats" || excluded[1] != "audit_events" {
		t.Fatalf("unexpected excludedTables audit details: %#v", details)
	}
	if strings.Contains(string(event.Details), passphrase) {
		t.Fatalf("audit details leaked passphrase: %s", string(event.Details))
	}
}

func TestDownloadDatabaseEncryptedRejectsMissingFixtureBackupPassphrase(t *testing.T) {
	settingService := initSessionTestDB(t)
	registerFixtureSettingsContributionForTest(t)
	registerFixtureBackupTransferCodecsForTest(t, settingService)
	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.GET("/api/getdb", withTestTokenScope("admin", "admin", (&ApiService{}).dbTransferHandler().DownloadDatabase))
	})
	recorder := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, "/api/getdb?backupEncryption=test-fixture-codec", nil), cookies...)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	obj, ok := msg.Obj.(map[string]any)
	if !ok || obj["errorClass"] != "missing_passphrase" {
		t.Fatalf("unexpected missing-passphrase response: %#v", msg.Obj)
	}
	var count int64
	if err := dbsqlite.DB().Model(&model.AuditEvent{}).Where("event = ?", "db_exported").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("missing passphrase should not export db, got %d export audit events", count)
	}
}

func saveFixtureBackupPassphrase(t *testing.T, settingService *service.SettingService, passphrase string) {
	t.Helper()
	registerFixtureSettingsContributionForTest(t)
	payload, err := json.Marshal(map[string]string{"fixtureBackupPassphrase": passphrase})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return settingService.Save(tx, payload)
	}); err != nil {
		t.Fatal(err)
	}
}

func newDatabaseImportRequest(t *testing.T, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("db", "backup.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	writeRestoreExecutionFields(t, writer, content)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/importdb", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func writeRestoreExecutionFields(t *testing.T, writer *multipart.Writer, plaintext []byte) {
	t.Helper()
	rehearsal, err := backup.Rehearse(t.Context(), bytes.NewReader(plaintext))
	if err != nil || !rehearsal.Possible {
		return
	}
	fields := map[string]string{
		"expectedRehearsalRevision": rehearsal.Revision,
		"idempotencyKey":            "restore-test-" + rehearsal.Revision[:24],
		"confirmation":              datalifecycle.RestoreConfirmation(rehearsal.Revision),
		"acknowledged":              "true",
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
}
