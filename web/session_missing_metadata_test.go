package web

import (
	"net/http"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
	ginsessions "github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func TestSQLiteSessionStoreRejectsMissingModernMetadataAfterRestart(t *testing.T) {
	db := initSQLiteSessionTestDB(t)
	var admin model.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	key := []byte("test-session-secret-32-bytes-long")
	store, err := NewSQLiteSessionStore(db, key)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	loginRouter := gin.New()
	loginRouter.Use(ginsessions.Sessions("s-ui", store))
	loginRouter.GET("/login", func(c *gin.Context) {
		session := ginsessions.Default(c)
		session.Set(service.SessionLoginUserKey, admin.Username)
		session.Set(service.SessionUserIDKey, uint64(admin.Id))
		session.Set(service.SessionRefKey, "missing-metadata-reference")
		session.Set(service.SessionAuthStateKey, service.AuthStateAuthenticated)
		session.Set(service.SessionAssuranceKey, service.AssurancePassword)
		session.Set(service.SessionLifetimePostureKey, service.LifetimePostureBoundedV1)
		session.Set(service.SessionCredentialGenerationKey, nonzeroSessionGeneration(admin.CredentialGeneration))
		session.Set(service.SessionMFAGenerationKey, nonzeroSessionGeneration(admin.MFAGeneration))
		session.Options(ginsessions.Options{Path: "/", HttpOnly: true})
		if err := session.Save(); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Status(http.StatusNoContent)
	})
	login := performSQLiteSessionRequest(loginRouter, "/login")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	if err := db.Where("ref = ?", "missing-metadata-reference").Delete(&model.SecuritySession{}).Error; err != nil {
		t.Fatal(err)
	}

	restarted, err := NewSQLiteSessionStore(db, key)
	if err != nil {
		t.Fatal(err)
	}
	readRouter := gin.New()
	readRouter.Use(ginsessions.Sessions("s-ui", restarted))
	readRouter.GET("/read", func(c *gin.Context) {
		if ginsessions.Default(c).Get(service.SessionLoginUserKey) != nil {
			c.Status(http.StatusNoContent)
			return
		}
		c.Status(http.StatusUnauthorized)
	})
	response := performSQLiteSessionRequest(readRouter, "/read", login.Result().Cookies()...)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing metadata status=%d", response.Code)
	}
	var backingRows int64
	if err := db.Table("sessions").Count(&backingRows).Error; err != nil {
		t.Fatal(err)
	}
	if backingRows != 0 {
		t.Fatalf("rejected session backing rows=%d", backingRows)
	}
}
