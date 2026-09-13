package api_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	panelapi "github.com/MalenkiySolovey/solovey-ui/api"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	panelweb "github.com/MalenkiySolovey/solovey-ui/web"
	ginsessions "github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func TestAPIRejectsModernSessionWhoseSQLiteMetadataIsMissing(t *testing.T) {
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "panel.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Errorf("close API session database: %v", err)
		}
	})
	if err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", "admin").Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}

	key := []byte("api-session-test-key-32-bytes-ok")
	newRouter := func() *gin.Engine {
		store, err := panelweb.NewSQLiteSessionStore(dbsqlite.DB(), key)
		if err != nil {
			t.Fatal(err)
		}
		router := gin.New()
		router.Use(ginsessions.Sessions("s-ui", store))
		router.GET("/login", func(c *gin.Context) {
			if err := panelapi.SetLoginUser(c, "admin", 0, ""); err != nil {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.Status(http.StatusNoContent)
		})
		router.GET("/protected", func(c *gin.Context) {
			if panelapi.GetLoginUser(c) != "admin" {
				c.Status(http.StatusUnauthorized)
				return
			}
			c.Status(http.StatusNoContent)
		})
		return router
	}
	request := func(router *gin.Engine, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		router.ServeHTTP(recorder, req)
		return recorder
	}

	router := newRouter()
	login := request(router, "/login")
	if login.Code != http.StatusNoContent || len(login.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d cookies=%d", login.Code, len(login.Result().Cookies()))
	}
	if response := request(router, "/protected", login.Result().Cookies()...); response.Code != http.StatusNoContent {
		t.Fatalf("valid SQLite session status=%d", response.Code)
	}
	var metadata model.SecuritySession
	if err := dbsqlite.DB().Where("username_snapshot = ?", "admin").Order("created_at DESC").First(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Where("session_id = ?", metadata.SessionID).Delete(&model.SecuritySession{}).Error; err != nil {
		t.Fatal(err)
	}

	restarted := newRouter()
	if response := request(restarted, "/protected", login.Result().Cookies()...); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing metadata API status=%d", response.Code)
	}
}
