package publicsurface

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type testHandler struct{ handled bool }

type panicHandler struct{}

func (panicHandler) ServePublic(*gin.Context, Context) bool { panic("secret") }

func (h testHandler) ServePublic(c *gin.Context, _ Context) bool {
	if !h.handled {
		return false
	}
	c.String(http.StatusOK, "public")
	return true
}

func TestRegistrySnapshotServesAndUnregisters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	unregister, err := Register("test-public-surface", testHandler{handled: true})
	if err != nil {
		t.Fatal(err)
	}
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
	unregister, err := Register("duplicate-owner", testHandler{handled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unregister)
	if _, err := Register("duplicate-owner", testHandler{handled: false}); err == nil {
		t.Fatal("duplicate owner registration was accepted")
	}
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

func TestServeContainsComponentHandlerPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	unregisterPanic, err := Register("panic-owner", panicHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer unregisterPanic()
	unregisterFallback, err := Register("safe-owner", testHandler{handled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer unregisterFallback()
	router := gin.New()
	router.NoRoute(func(c *gin.Context) {
		if !Serve(c, Context{}) {
			Handled404(c)
		}
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/public", nil))
	if response.Code != http.StatusOK || response.Body.String() != "public" {
		t.Fatalf("panicking optional handler escaped isolation: %d %q", response.Code, response.Body.String())
	}
}
