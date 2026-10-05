package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/gin-gonic/gin"
)

func TestCapabilityReadProjectionUsesOwnerAndScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, allow := range []bool{false, true} {
		router := gin.New()
		handler := NewHandler(Deps{
			RequireScope: func(c *gin.Context, resource string, allowed ...string) bool {
				if resource != "capabilities" || len(allowed) != 3 {
					t.Fatal("incorrect read scope contract")
				}
				if !allow {
					c.AbortWithStatus(http.StatusForbidden)
				}
				return allow
			},
			JSONObj: func(c *gin.Context, value interface{}, err error) { c.JSON(http.StatusOK, value) },
		})
		router.GET("/capabilities", handler.GetCapabilities)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
		if !allow {
			if rec.Code != http.StatusForbidden {
				t.Fatal("denied capability read")
			}
			continue
		}
		var snapshot entitycapabilities.Snapshot
		if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		owner := entitycapabilities.Current()
		if snapshot.Schema != owner.Schema || snapshot.ComponentProfile != owner.ComponentProfile || len(snapshot.Facts) != len(owner.Facts) {
			t.Fatalf("wire projection: %s", rec.Body.String())
		}
	}
}
