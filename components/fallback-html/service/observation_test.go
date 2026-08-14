//go:build !minimal

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	"github.com/gin-gonic/gin"
)

func TestServePublicEmitsOnlySanitizedObservationClasses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	subscription, unregister, err := publicsurface.SubscribeObservations(4)
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	runtime := NewRuntime()
	runtime.snapshot.Store(&snapshot{
		siteID: 42,
		pages: map[string]publishedFile{
			"/": {publicPath: "/", mimeType: "text/html", sha256: "fixture", data: []byte("ok")},
		},
		redirects: map[string]publishedRedirect{},
		csp:       "default-src 'self'",
	})
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodTrace, "/?token=must-not-leak", nil)
	context.Request.Header.Set("User-Agent", "curl/8.0 secret")
	context.Request.RemoteAddr = "203.0.113.10:4444"
	if !runtime.ServePublic(context, publicsurface.Context{AdminBasePath: "/private-admin/"}) {
		t.Fatal("fallback request was not handled")
	}
	select {
	case observation := <-subscription.Observations():
		if observation.ResourceID != "component:fallback-html:site:42" || observation.PathClass != "fallback_path" || observation.UserAgentClass != "ua_scanner" || observation.MethodClass != "unexpected" {
			t.Fatalf("observation = %#v", observation)
		}
		if observation.SourceIP != "203.0.113.10" {
			t.Fatalf("source IP = %q", observation.SourceIP)
		}
	case <-time.After(time.Second):
		t.Fatal("observation was not emitted")
	}
}
