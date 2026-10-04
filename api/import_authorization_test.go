package api

import (
	"net/http"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
)

func TestImportCompletionCapabilitiesRefreshOwnedTokenSnapshot(t *testing.T) {
	initSessionTestDB(t)
	completeTokenOwnerResetForTest(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v2 := NewAPIv2Handler(router.Group("/apiv2"))
	browser := &APIHandler{ApiService: v2.ApiService, apiv2: v2}
	for name, completion := range map[string]func(){
		"browser component":     browser.componentAPI(nil, browser.reloadTokens).Auth.AuthorizationChanged,
		"bearer component":      v2.componentAPI(nil, v2.ReloadTokens).Auth.AuthorizationChanged,
		"native bearer restore": v2.db.AuthorizationChanged,
	} {
		t.Run(name, func(t *testing.T) {
			if completion == nil {
				t.Fatal("authorization completion capability is missing")
			}
			token := model.Tokens{Desc: "imported", Token: "import-fixture-token", UserId: 1, Scope: "read"}
			if err := dbsqlite.DB().Create(&token).Error; err != nil {
				t.Fatal(err)
			}
			if result := performAPIV2TokenRequest(router, "Authorization", "Bearer import-fixture-token"); result.Code != http.StatusUnauthorized {
				t.Fatal("token entered the snapshot without completion")
			}
			completion()
			if result := performAPIV2TokenRequest(router, "Authorization", "Bearer import-fixture-token"); result.Code != http.StatusOK {
				t.Fatalf("completed token not available: %d", result.Code)
			}
			if err := dbsqlite.DB().Delete(&token).Error; err != nil {
				t.Fatal(err)
			}
			completion()
			v2.tokensMu.RLock()
			count := len(v2.tokens)
			v2.tokensMu.RUnlock()
			if count != 0 {
				t.Fatal("removed authority remained in snapshot")
			}
		})
	}
	(&APIHandler{}).componentAPI(nil, (&APIHandler{}).reloadTokens).Auth.AuthorizationChanged()
}
