package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestActionAdapterPreservesRealSessionCSRFPlanAndOneUseStepUp(t *testing.T) {
	resetRateLimitState()
	settings := initSessionTestDB(t)
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	const password = "Semantic action test administrator 2026!"
	if err := (&service.UserService{}).UpdateFirstUser("admin", password); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	store := newAPITestSessionStore(t)
	store.Options(sessions.Options{Path: "/", HttpOnly: true, Secure: true})
	router.Use(sessions.Sessions("s-ui", store))
	apiv2 := NewAPIv2Handler(router.Group("/apiv2"))
	handler := &APIHandler{ApiService: NewApiService(), apiv2: apiv2}
	group := router.Group("/api")
	handler.initRouter(group)
	accepted := 0
	revision := strings.Repeat("a", 64)
	// This fixed test-only owner command exercises the actual production
	// authority adapter under the real middleware; no generic product API exists.
	group.POST("/action-policy-proof", func(c *gin.Context) {
		actual := actions.Restore(operationcoordination.NewReference("data", "database", "data-operation:policy-proof"), actions.RestoreInput{RehearsalRevision: revision})
		if _, err := actions.Require(c.Request.Context(), actual, handler.actionPolicy(handler.requireStepUpAction)(c)); err != nil {
			if !c.IsAborted() {
				c.AbortWithStatus(http.StatusForbidden)
			}
			return
		}
		accepted++
		c.JSON(http.StatusOK, Msg{Success: true})
	})
	const path = "/api/action-policy-proof"
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", password)
	csrf := adminFlowCSRFToken(t, router, jar)
	missing := adminFlowPost(t, router, jar, csrf, "", path, url.Values{})
	if missing.Code != http.StatusForbidden || accepted != 0 {
		t.Fatal("missing step-up reached owner")
	}
	grant, csrf := adminFlowStepUp(t, router, jar, password, actions.BackupRestore, "database:restore:"+revision)
	revision = strings.Repeat("b", 64)
	changed := adminFlowPost(t, router, jar, csrf, grant, path, url.Values{})
	if changed.Code != http.StatusForbidden || accepted != 0 {
		t.Fatal("approved plan authorized changed input")
	}
	revision = strings.Repeat("a", 64)
	assertAdminFlowMsgSuccess(t, adminFlowPost(t, router, jar, csrf, grant, path, url.Values{}), true)
	replay := adminFlowPost(t, router, jar, csrf, grant, path, url.Values{})
	if replay.Code != http.StatusForbidden || accepted != 1 {
		t.Fatal("consumed grant authorized another owner command")
	}

	grant, csrf = adminFlowStepUp(t, router, jar, password, actions.BackupRestore, "database:restore:"+revision)
	noCSRF := adminFlowPost(t, router, jar, "", grant, path, url.Values{})
	if noCSRF.Code != http.StatusForbidden || accepted != 1 {
		t.Fatal("action adapter bypassed CSRF")
	}
	assertAdminFlowMsgSuccess(t, adminFlowPost(t, router, jar, csrf, grant, path, url.Values{}), true)
	if accepted != 2 {
		t.Fatal("CSRF rejection consumed or accepted the grant")
	}

	grant, csrf = adminFlowStepUp(t, router, jar, password, actions.BackupRestore, "database:restore:"+revision)
	secondJar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, secondJar, "admin", password)
	secondCSRF := adminFlowCSRFToken(t, router, secondJar)
	foreignSession := adminFlowPost(t, router, secondJar, secondCSRF, grant, path, url.Values{})
	if foreignSession.Code != http.StatusForbidden || accepted != 2 {
		t.Fatal("grant crossed its issuing session")
	}
	assertAdminFlowMsgSuccess(t, adminFlowPost(t, router, jar, csrf, grant, path, url.Values{}), true)
	if accepted != 3 {
		t.Fatal("exact issuing session could not use its unconsumed grant")
	}

	grant, csrf = adminFlowStepUp(t, router, jar, password, actions.BackupRestore, "database:restore:"+revision)
	if err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", "admin").UpdateColumn("mfa_generation", gorm.Expr("mfa_generation + 1")).Error; err != nil {
		t.Fatal(err)
	}
	revoked := adminFlowPost(t, router, jar, csrf, grant, path, url.Values{})
	if accepted != 3 {
		t.Fatal("stale MFA/session generation reached owner")
	}
	assertAdminFlowMsgSuccess(t, revoked, false)
}
