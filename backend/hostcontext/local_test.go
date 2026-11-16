package hostcontext

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/gin-gonic/gin"
)

func TestLocalHostRequestBinding(t *testing.T) {
	cases := []struct {
		name    string
		values  []string
		allowed bool
	}{
		{name: "absent", allowed: true},
		{name: "local", values: []string{"local"}, allowed: true},
		{name: "foreign", values: []string{"other"}},
		{name: "explicit empty", values: []string{""}},
		{name: "comma separated", values: []string{"local,other"}},
		{name: "duplicate local", values: []string{"local", "local"}},
		{name: "duplicate mixed", values: []string{"local", "other"}},
		{name: "malformed case", values: []string{"LOCAL"}},
		{name: "malformed delimiter", values: []string{"local;other"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			for _, value := range tc.values {
				request.Header.Add(Header, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if tc.allowed {
				if ownerCalls != 1 || response.Code != http.StatusNoContent || response.Header().Get(Header) != "local" {
					t.Fatalf("Local response: %d, calls %d, header %q", response.Code, ownerCalls, response.Header().Get(Header))
				}
			} else if ownerCalls != 0 || response.Code != http.StatusNotFound {
				t.Fatalf("foreign Host reached owner: %d, calls %d", response.Code, ownerCalls)
			}
		})
	}
}

func TestLocalHostBindingPreservesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	router := gin.New()
	router.Use(Middleware())
	router.GET("/read", func(c *gin.Context) {
		if !errors.Is(c.Request.Context().Err(), context.Canceled) {
			t.Fatalf("request cancellation was not preserved: %v", c.Request.Context().Err())
		}
		if host, ok := FromContext(c.Request.Context()); !ok || host != hostref.Local() {
			t.Fatalf("cancelled request lost Local Host binding: %+v %v", host, ok)
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/read", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("cancelled request status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestFromContextRejectsMissingContext(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"nil":        nil,
		"background": context.Background(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := FromContext(ctx); ok {
				t.Fatal("missing Host context accepted")
			}
		})
	}
}
