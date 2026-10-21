package hostcontext

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	"github.com/gin-gonic/gin"
)

func TestLocalHostRequestBinding(t *testing.T) {
	for _, values := range [][]string{nil, {"local"}, {"other"}, {""}, {"local,other"}, {"local", "other"}} {
		t.Run(http.Header{Header: values}.Get(Header), func(t *testing.T) {
			ownerCalls := 0
			router := gin.New()
			router.Use(Middleware())
			router.POST("/write", func(c *gin.Context) {
				host, ok := FromContext(c.Request.Context())
				if !ok || host != hostref.Local() {
					t.Fatalf("unbound owner Host: %+v %v", host, ok)
				}
				ownerCalls++
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, "/write", nil)
			for _, value := range values {
				request.Header.Add(Header, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			allowed := len(values) == 0 || (len(values) == 1 && values[0] == "local")
			if allowed {
				if ownerCalls != 1 || response.Code != http.StatusNoContent || response.Header().Get(Header) != "local" {
					t.Fatalf("Local response: %d, calls %d", response.Code, ownerCalls)
				}
			} else if ownerCalls != 0 || response.Code != http.StatusNotFound {
				t.Fatalf("foreign Host reached owner: %d, calls %d", response.Code, ownerCalls)
			}
		})
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("missing Host context accepted")
	}
}
