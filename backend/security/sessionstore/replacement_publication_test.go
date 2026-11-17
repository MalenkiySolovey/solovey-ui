package sessionstore

import (
	"net/http"
	"testing"

	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	ginsessions "github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func TestReplacementPublishesRealtimeInvalidationOnlyAfterCommit(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[reject], func(t *testing.T) {
			db := initSQLiteSessionTestDB(t)
			var admin model.User
			if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
				t.Fatal(err)
			}
			store, err := NewSQLiteSessionStore(db, []byte("test-session-secret-32-bytes-long"))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			store.CloseSession = func(ref, reason string) {
				calls++
				if ref != "old-reference" || reason != "session_replaced" {
					t.Fatal("wrong session invalidation authority")
				}
				var previous, next model.SecuritySession
				if err := db.Where("ref = ?", ref).First(&previous).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Where("ref = ?", "new-reference").First(&next).Error; err != nil {
					t.Fatal("replacement was not committed before realtime invalidation")
				}
				if previous.State != panelsecurity.SessionStateRevoked || previous.ReplacementRef != next.Ref {
					t.Fatal("old session was not retired before realtime invalidation")
				}
			}
			router := gin.New()
			router.Use(ginsessions.Sessions("s-ui", store))
			setValues := func(c *gin.Context, ref string) {
				s := ginsessions.Default(c)
				s.Set(panelsecurity.SessionLoginUserKey, admin.Username)
				s.Set(panelsecurity.SessionRefKey, ref)
				s.Set(panelsecurity.SessionUserIDKey, uint64(admin.Id))
				s.Set(panelsecurity.SessionCredentialGenerationKey, uint64(1))
				s.Set(panelsecurity.SessionMFAGenerationKey, uint64(1))
				s.Options(ginsessions.Options{Path: "/", HttpOnly: true})
			}
			router.GET("/seed", func(c *gin.Context) {
				setValues(c, "old-reference")
				if err := ginsessions.Default(c).Save(); err != nil {
					t.Fatal(err)
				}
				c.Status(204)
			})
			router.GET("/replace", func(c *gin.Context) {
				setValues(c, "new-reference")
				s := ginsessions.Default(c)
				s.Set(panelsecurity.SessionRegenerateKey, true)
				if err := s.Save(); err != nil {
					c.Status(500)
					return
				}
				c.Status(204)
			})
			seed := performSQLiteSessionRequest(router, "/seed")
			if seed.Code != 204 {
				t.Fatal(seed.Code)
			}
			if reject {
				if err := db.Exec("CREATE TRIGGER reject_replacement BEFORE INSERT ON security_sessions WHEN NEW.ref = 'new-reference' BEGIN SELECT RAISE(ABORT, 'fixture rollback'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			response := performSQLiteSessionRequest(router, "/replace", seed.Result().Cookies()...)
			if !reject {
				if response.Code != http.StatusNoContent || calls != 1 {
					t.Fatal("committed replacement did not publish exactly once")
				}
				return
			}
			if response.Code != http.StatusInternalServerError || calls != 0 {
				t.Fatal("failed replacement published invalidation")
			}
			var previous model.SecuritySession
			if err := db.Where("ref = ?", "old-reference").First(&previous).Error; err != nil {
				t.Fatal(err)
			}
			if previous.State != panelsecurity.SessionStateActive || previous.RevokedAt != 0 {
				t.Fatal("failed replacement retired the accepted session")
			}
			var count int64
			if err := db.Table("sessions").Count(&count).Error; err != nil || count != 1 {
				t.Fatal("replacement rollback did not preserve exactly the previous backing row")
			}
		})
	}
}
