package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/deposist/s-ui-x/config"
	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/importxui"
	"github.com/deposist/s-ui-x/database/model"
	"github.com/deposist/s-ui-x/service"
	"github.com/gin-gonic/gin"
)

func TestIssue7XUISyncProfilesAPIReturnsImportPolicyFields(t *testing.T) {
	settingService := setupClusterEAPITestDB(t)
	router, cookies := newAuthenticatedTestRouter(t, settingService, func(router *gin.Engine) {
		router.POST("/api/import-xui/sync/profiles", withTestTokenScope("remote-token", "xui_remote", (&ApiService{}).SaveXUISyncProfile))
		router.GET("/api/import-xui/sync/profiles", withTestTokenScope("remote-token", "xui_remote", (&ApiService{}).XUISyncProfiles))
	})
	// A scoped xui_remote token may only save SSRF-confinable http(s) sources;
	// file/ssh are admin-only (M3/M4). This test exercises the import-policy field
	// round-trip over the scoped-token path, so it uses a public http source.
	body := strings.NewReader(`{
		"name":"issue7-api-policy",
		"sourceType":"xuihttp",
		"strategy":"replace",
		"onlyNew":false,
		"enabled":true,
		"includeSettings":true,
		"includeHistory":true,
		"includeRouting":true,
		"adminMode":"reset_required",
		"schedule":"0 */6 * * *",
		"source":{"type":"xuihttp","baseUrl":"http://1.1.1.1:2053","username":"admin","password":"pw"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/import-xui/sync/profiles", body)
	req.Header.Set("Content-Type", "application/json")
	saveRecorder := performAuthenticatedTestRequest(router, req, cookies...)
	if saveRecorder.Code != http.StatusOK {
		t.Fatalf("save profile status=%d body=%s", saveRecorder.Code, saveRecorder.Body.String())
	}

	var stored model.XUISyncProfile
	if err := database.GetDB().Where("name = ?", "issue7-api-policy").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.OnlyNew || !stored.IncludeSettings || !stored.IncludeHistory || !stored.IncludeRouting || stored.AdminMode != string(importxui.AdminModeResetRequired) {
		t.Fatalf("stored profile policy mismatch: %#v", stored)
	}

	listRecorder := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, "/api/import-xui/sync/profiles", nil), cookies...)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list profiles status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var msg struct {
		Success bool                   `json:"success"`
		Obj     []model.XUISyncProfile `json:"obj"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success || len(msg.Obj) == 0 {
		t.Fatalf("unexpected list response: %s", listRecorder.Body.String())
	}
	got := msg.Obj[0]
	if got.OnlyNew || !got.IncludeSettings || !got.IncludeHistory || !got.IncludeRouting || got.AdminMode != string(importxui.AdminModeResetRequired) {
		t.Fatalf("GET profile policy fields mismatch: %#v", got)
	}
}

func setupClusterEAPITestDB(t *testing.T) *service.SettingService {
	t.Helper()
	closeAPITestDB(t)
	xuiRateMu.Lock()
	xuiRates = map[string]xuiAttempt{}
	xuiRateMu.Unlock()
	prevAuditSync := service.AuditSyncForTest
	service.AuditSyncForTest = true
	t.Cleanup(func() { service.AuditSyncForTest = prevAuditSync })
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	t.Setenv("XUI_DISABLE_REMOTE", "")
	initAPITestDB(t, config.GetDBPath())
	t.Cleanup(func() {
		stopTokenUseDebouncerBeforeAPITestDBInit(t)
		if testDB := database.GetDB(); testDB != nil {
			if sqlDB, err := testDB.DB(); err == nil {
				_ = sqlDB.Close()
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
	return &service.SettingService{}
}
