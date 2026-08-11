package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func TestRestoreEndpointAcceptsPlaintextDatabaseBackup(t *testing.T) {
	settingService := initRestoreTestDB(t)
	withNoopSighup(t)
	if err := setRestoreMarker("plaintext-backup"); err != nil {
		t.Fatal(err)
	}
	backup, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	if err := setRestoreMarker("live-before-import"); err != nil {
		t.Fatal(err)
	}

	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.POST("/api/importdb", withTestTokenScope("admin", "admin", databaseImportTestHandler()))
	})
	recorder := performAuthenticatedTestRequest(router, newDatabaseImportRequest(t, backup), cookies...)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertRestoreSuccess(t, recorder)
	if got := restoreMarkerValue(t); got != "plaintext-backup" {
		t.Fatalf("plaintext import did not restore marker, got %q", got)
	}
}

func TestRestoreEndpointDecryptsComponentBackupEnvelope(t *testing.T) {
	settingService := initRestoreTestDB(t)
	registerFixtureBackupTransferCodecsForTest(t, settingService)
	withNoopSighup(t)
	if err := setRestoreMarker("encrypted-backup"); err != nil {
		t.Fatal(err)
	}
	backup, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("correct horse battery staple")
	envelope, err := backupenvelope.Build(backup, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if err := setRestoreMarker("live-before-import"); err != nil {
		t.Fatal(err)
	}

	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.POST("/api/importdb", withTestTokenScope("admin", "admin", databaseImportTestHandler()))
	})
	req := newDatabaseImportRequestWithPassphrase(t, envelope, string(passphrase))
	recorder := performAuthenticatedTestRequest(router, req, cookies...)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertRestoreSuccess(t, recorder)
	if got := restoreMarkerValue(t); got != "encrypted-backup" {
		t.Fatalf("encrypted import did not restore marker, got %q", got)
	}
}

func TestRestoreEndpointRejectsBadComponentBackupPassphraseWithoutTouchingLiveDB(t *testing.T) {
	settingService := initRestoreTestDB(t)
	registerFixtureBackupTransferCodecsForTest(t, settingService)
	withNoopSighup(t)
	if err := setRestoreMarker("encrypted-backup"); err != nil {
		t.Fatal(err)
	}
	backup, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := backupenvelope.Build(backup, []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	if err := setRestoreMarker("live-before-import"); err != nil {
		t.Fatal(err)
	}

	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.POST("/api/importdb", withTestTokenScope("admin", "admin", databaseImportTestHandler()))
	})
	req := newDatabaseImportRequestWithPassphrase(t, envelope, "wrong horse battery staple")
	recorder := performAuthenticatedTestRequest(router, req, cookies...)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertRestoreFailureClass(t, recorder, "decryption_failed")
	if got := restoreMarkerValue(t); got != "live-before-import" {
		t.Fatalf("failed decrypt touched live DB, marker=%q", got)
	}

	flushAPIAudit(t)

	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "fixture_backup_restore_failed").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	details := string(event.Details)
	if !strings.Contains(details, `"errorClass":"decryption_failed"`) {
		t.Fatalf("unexpected restore audit details: %s", details)
	}
	if strings.Contains(details, "wrong horse") || strings.Contains(details, "correct horse") {
		t.Fatalf("restore audit leaked passphrase: %s", details)
	}
}

func initRestoreTestDB(t *testing.T) *service.SettingService {
	t.Helper()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	return settingService
}

func newDatabaseImportRequestWithPassphrase(t *testing.T, content []byte, passphrase string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("db", "backup.db.aes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if passphrase != "" {
		if err := writer.WriteField("fixtureBackupPassphrase", passphrase); err != nil {
			t.Fatal(err)
		}
	}
	if plaintext, err := backupenvelope.Open(content, []byte(passphrase)); err == nil {
		writeRestoreExecutionFields(t, writer, plaintext)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/importdb", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func withNoopSighup(t *testing.T) {
	t.Helper()
	backup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { backup.SetSendSighupHook(nil) })
	t.Cleanup(func() {
		if db := dbsqlite.DB(); db != nil {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
}

func setRestoreMarker(value string) error {
	db := dbsqlite.DB()
	if err := db.Where("key = ?", "restore_marker").Delete(&model.Setting{}).Error; err != nil {
		return err
	}
	return db.Create(&model.Setting{Key: "restore_marker", Value: value}).Error
}

func restoreMarkerValue(t *testing.T) string {
	t.Helper()
	var setting model.Setting
	if err := dbsqlite.DB().Where("key = ?", "restore_marker").Order("id desc").First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	return setting.Value
}

func assertRestoreSuccess(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success {
		t.Fatalf("expected restore success, got %#v body=%s", msg, recorder.Body.String())
	}
}

func assertRestoreFailureClass(t *testing.T, recorder *httptest.ResponseRecorder, wantClass string) {
	t.Helper()
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Success {
		t.Fatalf("expected restore failure, got %#v", msg)
	}
	obj, ok := msg.Obj.(map[string]any)
	if !ok || obj["errorClass"] != wantClass {
		t.Fatalf("unexpected restore failure obj: %#v", msg.Obj)
	}
}
