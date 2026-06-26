package failover

import (
	"net/http"
	"net/http/httptest"
	"testing"

	servicefailover "github.com/MalenkiySolovey/solovey-ui/service/failover"
	"github.com/gin-gonic/gin"
)

func TestRegisterRoutesServesStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api")
	called := false
	RegisterRoutes(group, Deps{
		Status: func() ([]servicefailover.StatusEntry, error) {
			called = true
			return []servicefailover.StatusEntry{{Tag: "group"}}, nil
		},
		JSONObj: func(context *gin.Context, value any, err error) {
			if err != nil {
				t.Fatal(err)
			}
			context.JSON(http.StatusOK, value)
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/failover-status", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !called {
		t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body.String())
	}
}
