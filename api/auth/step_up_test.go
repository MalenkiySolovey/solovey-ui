package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSensitiveAuthMutationFailsClosedWithoutStepUpCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/addToken", nil)
	handler := NewHandler(Deps{
		LoginUser: func(*gin.Context) string { return "admin" },
	})

	handler.AddToken(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("sensitive auth mutation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
