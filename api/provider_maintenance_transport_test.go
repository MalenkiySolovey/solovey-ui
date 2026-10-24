package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Cross a real native transport deadline; a recorder alone cannot reproduce
// the installed CLI losing an otherwise successful maintenance response.
func TestProviderMaintenanceResponseSurvivesOrdinaryWriteTimeout(t *testing.T) {
	for _, protocol := range []string{"http1", "http2"} {
		t.Run(protocol, func(t *testing.T) {
			ready, release := make(chan struct{}), make(chan struct{})
			router := gin.New()
			router.POST("/maintain", func(c *gin.Context) {
				ctx, cancel := context.WithTimeout(c.Request.Context(), providerMaintenanceRequestTimeout)
				defer cancel()
				bindProviderMaintenanceResponseDeadline(c, ctx)
				close(ready)
				select {
				case <-release:
					jsonObj(c, gin.H{"state": "VERIFYING_ACTIVE"}, nil)
				case <-ctx.Done():
				}
			})
			server := httptest.NewUnstartedServer(router)
			const ordinaryWindow = 100 * time.Millisecond
			server.Config.WriteTimeout = ordinaryWindow
			if protocol == "http2" {
				server.EnableHTTP2 = true
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/maintain", nil)
			result := make(chan error, 1)
			go func() {
				response, err := server.Client().Do(request)
				if err == nil {
					defer response.Body.Close()
					var envelope struct {
						Success bool `json:"success"`
					}
					err = json.NewDecoder(response.Body).Decode(&envelope)
					if err == nil && (response.StatusCode != http.StatusOK || !envelope.Success || protocol == "http2" && response.ProtoMajor != 2) {
						err = context.Canceled
					}
				}
				result <- err
			}()
			select {
			case <-ready:
			case <-ctx.Done():
				t.Fatal("maintenance did not reach the native transport")
			}
			select {
			case <-time.After(2 * ordinaryWindow):
			case <-ctx.Done():
				t.Fatal("maintenance transport fixture timed out")
			}
			close(release)
			select {
			case err := <-result:
				if err != nil {
					t.Fatal("maintenance response was lost at the ordinary write deadline", err)
				}
			case <-ctx.Done():
				t.Fatal("bounded maintenance response was not delivered")
			}
		})
	}
}

func TestProviderMaintenanceResponsePreservesShorterCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(time.Minute))
	defer cancel()
	deadline, _ := ctx.Deadline()
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	var writes []time.Time
	context.Writer = providerMaintenanceDeadlineWriter{ResponseWriter: context.Writer, writes: &writes}
	bindProviderMaintenanceResponseDeadline(context, ctx)
	if len(writes) != 1 || !writes[0].Equal(deadline.Add(10*time.Second)) {
		t.Fatal("shorter caller deadline was replaced", writes)
	}
}
