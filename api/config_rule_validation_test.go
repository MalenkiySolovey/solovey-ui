package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
)

func TestConfigSaveHTTPRejectsInvalidRuleWithSafePathAndCode(t *testing.T) {
	settings := initSessionTestDB(t)
	before, err := settings.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	router, cookies := newAuthenticatedTestRouter(t, settings, func(router *gin.Engine) {
		router.POST("/api/save", func(c *gin.Context) {
			(&ApiService{}).configHandler().Save(c, "admin")
		})
	})
	form := url.Values{"object": {"config"}, "action": {"set"}, "data": {`{"route":{"rules":[{"type":"logical","mode":"and","rules":[{"password-fixture-marker":"secret-fixture-marker"}]}]}}`}}
	request := httptest.NewRequest(http.MethodPost, "/api/save", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := performAuthenticatedTestRequest(router, request, cookies...)
	var reply struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || !strings.Contains(reply.Msg, "route.rules[0].rules[0]") || !strings.Contains(reply.Msg, "invalid_rule") {
		t.Fatalf("HTTP lost the owner error: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "secret-fixture-marker") || strings.Contains(recorder.Body.String(), "password-fixture-marker") {
		t.Fatal("HTTP response exposed submitted values")
	}
	after, err := settings.GetConfig()
	if err != nil || after != before {
		t.Fatalf("rejected HTTP save changed stored config: %v", err)
	}
	var changes int64
	if err := dbsqlite.DB().Model(&model.Changes{}).Where("key = ?", "config").Count(&changes).Error; err != nil || changes != 0 {
		t.Fatalf("rejected HTTP save recorded a committed config change: count=%d err=%v", changes, err)
	}
}
