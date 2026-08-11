package publicsurface

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type testHandler struct{ handled bool }

func (h testHandler) ServePublic(c *gin.Context, _ Context) bool {
	if !h.handled {
		return false
	}
	c.String(http.StatusOK, "public")
	return true
}

func TestRegistrySnapshotServesAndUnregisters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	unregister := Register("test-public-surface", testHandler{handled: true})
	router := gin.New()
	router.NoRoute(func(c *gin.Context) {
		if Serve(c, Context{AdminBasePath: "/app/"}) {
			return
		}
		Handled404(c)
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/public", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "public" {
		t.Fatalf("public response = %d %q", response.Code, response.Body.String())
	}

	unregister()
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("after unregister status = %d, want 404", response.Code)
	}
}

func TestDuplicateOwnerCannotReplaceOrUnregisterExistingHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	unregister := Register("duplicate-owner", testHandler{handled: true})
	t.Cleanup(unregister)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("duplicate owner registration did not panic")
			}
		}()
		Register("duplicate-owner", testHandler{handled: false})
	}()
	router := gin.New()
	router.NoRoute(func(c *gin.Context) {
		if Serve(c, Context{}) {
			return
		}
		Handled404(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/public", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("duplicate registration displaced original handler: %d", response.Code)
	}
}
