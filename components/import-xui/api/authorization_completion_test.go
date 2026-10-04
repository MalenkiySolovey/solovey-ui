//go:build !minimal

package importxui

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbimport "github.com/MalenkiySolovey/solovey-ui/components/import-xui/database"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func TestImportAuthorizationRefreshOccursAfterCommitBeforeEffects(t *testing.T) {
	initImportXUIAPITestDB(t)
	ResetRateLimits()
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Tokens{UserId: 1, Token: "local-fixture-token", Scope: "read"}).Error; err != nil {
		t.Fatal(err)
	}
	users := &service.UserService{}
	snapshot, err := users.LoadTokens()
	if err != nil || importTokenCount(t, snapshot) == 0 {
		t.Fatal("baseline token unavailable", err)
	}
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	source, err := sql.Open("sqlite3", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	for _, statement := range []string{
		"CREATE TABLE inbounds (id INTEGER)", "CREATE TABLE client_traffics (id INTEGER)",
		"CREATE TABLE users (id INTEGER, username TEXT, password TEXT)",
		"INSERT INTO users VALUES (1, 'admin', 'source-fixture-password')",
	} {
		if _, err := source.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := dbimport.Plan(sourcePath, dbimport.PlanOptions{Strategy: dbimport.StrategyMerge, AdminMode: dbimport.AdminModeResetRequired})
	if err != nil {
		t.Fatal(err)
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	refreshes, effects, audits := 0, 0, 0
	handler := NewHandler(Deps{
		RequireScope:  func(*gin.Context, string, ...string) bool { return true },
		RequireStepUp: func(*gin.Context, string, string) bool { return true },
		Actor:         func(*gin.Context) string { return "admin" }, RemoteIP: func(*gin.Context) string { return "192.0.2.1" },
		AuthorizationChanged: func() {
			refreshes++
			var owner model.User
			if err := dbsqlite.DB().First(&owner, 1).Error; err != nil || !owner.ForcePasswordReset {
				t.Fatal("refresh preceded account commit", err)
			}
			snapshot, err = users.LoadTokens()
			if err != nil || importTokenCount(t, snapshot) != 0 {
				t.Fatal("reset owner remained authorized", err)
			}
		},
		Audit: func(_ *gin.Context, _, event, _, _ string, _ map[string]any) {
			if event == "panel_import" {
				audits++
				if refreshes != 1 {
					t.Fatal("success audit preceded refresh")
				}
			}
		},
		ConfigChanged: func() {
			effects++
			if refreshes != 1 {
				t.Fatal("config effect preceded refresh")
			}
		},
		JSONObj: func(c *gin.Context, obj interface{}, err error) {
			c.JSON(http.StatusOK, Envelope{Success: err == nil, Obj: obj})
		},
	})
	router := gin.New()
	router.POST("/import", handler.ImportXui)
	router.POST("/plan", handler.ImportXuiPlan)
	router.POST("/apply", handler.ImportXuiApply)
	for _, request := range []*http.Request{
		importCompletionRequest(t, "/plan", content, map[string]string{"adminMode": "reset_required"}),
		importCompletionRequest(t, "/import", content, map[string]string{"dryRun": "1"}),
		importCompletionRequest(t, "/apply", []byte("not SQLite"), map[string]string{"plan": string(planJSON)}),
	} {
		ResetRateLimits()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK && recorder.Code != http.StatusBadRequest {
			t.Fatalf("preview/failure status=%d", recorder.Code)
		}
		if refreshes != 0 || effects != 0 || audits != 0 {
			t.Fatal("preview/failure emitted committed effects")
		}
		current, err := users.LoadTokens()
		if err != nil || !bytes.Equal(snapshot, current) {
			t.Fatal("preview/failure changed authorization", err)
		}
	}
	ResetRateLimits()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, importCompletionRequest(t, "/apply", content, map[string]string{"plan": string(planJSON)}))
	var response Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !response.Success || refreshes != 1 || effects != 1 || audits != 1 {
		t.Fatalf("commit status=%d success=%v refreshes=%d effects=%d audits=%d", recorder.Code, response.Success, refreshes, effects, audits)
	}
}

func TestImportRollbackRefreshesAcceptedAuthorizationBeforeAudit(t *testing.T) {
	initImportXUIAPITestDB(t)
	t.Cleanup(func() { _ = dbsqlite.Close() })
	ResetRateLimits()
	backup.SetSendSighupHook(func() error { return errors.New("restart deferred") })
	t.Cleanup(func() { backup.SetSendSighupHook(nil) })
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Tokens{UserId: 1, Token: "rollback-fixture-token", Scope: "read"}).Error; err != nil {
		t.Fatal(err)
	}
	content, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	const reference = "s-ui-pre-xui-import-1790000000.db"
	if err := os.WriteFile(filepath.Join(configstorage.GetDBFolderPath(), reference), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", 1).Update("force_password_reset", true).Error; err != nil {
		t.Fatal(err)
	}
	refreshes := 0
	handler := NewHandler(Deps{
		RequireScope:  func(*gin.Context, string, ...string) bool { return true },
		RequireStepUp: func(*gin.Context, string, string) bool { return true },
		Actor:         func(*gin.Context) string { return "admin" }, RemoteIP: func(*gin.Context) string { return "192.0.2.1" },
		AuthorizationChanged: func() {
			refreshes++
			snapshot, err := (&service.UserService{}).LoadTokens()
			if err != nil || importTokenCount(t, snapshot) != 1 {
				t.Fatal("accepted rollback authority unavailable", err)
			}
		},
		Audit: func(_ *gin.Context, _, event, _, _ string, _ map[string]any) {
			if event == "panel_import_rollback" && refreshes != 1 {
				t.Fatal("rollback audit preceded authorization refresh")
			}
		},
		ConfigChanged: func() {},
		JSONMsg:       func(c *gin.Context, _ string, err error) { c.JSON(http.StatusOK, Envelope{Success: err == nil}) },
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/rollback", strings.NewReader(url.Values{"backup": {reference}}.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ImportXuiRollback(c)
	var result Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !result.Success || refreshes != 1 {
		t.Fatalf("rollback status=%d success=%v refreshes=%d", recorder.Code, result.Success, refreshes)
	}
}

func TestImportMutationsFailClosedWithoutAuthorizationCapability(t *testing.T) {
	ResetRateLimits()
	handler := NewHandler(Deps{
		RequireScope:  func(*gin.Context, string, ...string) bool { return true },
		RequireStepUp: func(*gin.Context, string, string) bool { return true },
		Actor:         func(*gin.Context) string { return "admin" }, RemoteIP: func(*gin.Context) string { return "192.0.2.1" },
		Audit:   func(*gin.Context, string, string, string, string, map[string]any) {},
		JSONObj: func(*gin.Context, interface{}, error) {}, JSONMsg: func(*gin.Context, string, error) {}, ConfigChanged: func() {},
	})
	for _, action := range []gin.HandlerFunc{handler.ImportXuiApply, handler.ImportXuiRollback} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/mutation", nil)
		action(c)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("missing refresh status=%d", recorder.Code)
		}
	}
}

func importCompletionRequest(t *testing.T, path string, content []byte, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("db", "source.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func importTokenCount(t *testing.T, snapshot []byte) int {
	t.Helper()
	var tokens []json.RawMessage
	if err := json.Unmarshal(snapshot, &tokens); err != nil {
		t.Fatal("invalid authorization snapshot")
	}
	return len(tokens)
}
