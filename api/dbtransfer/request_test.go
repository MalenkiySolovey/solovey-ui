package dbtransfer

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type transferBodyBarrier struct {
	io.ReadCloser
	once  sync.Once
	ready chan time.Time
	c     *gin.Context
}

func (b *transferBodyBarrier) Read(p []byte) (int, error) {
	b.once.Do(func() { deadline, _ := b.c.Request.Context().Deadline(); b.ready <- deadline })
	return b.ReadCloser.Read(p)
}

// The actual handler parses a delayed multipart body across a real server's
// ordinary write timeout. Its refusal of an invalid archive must still reach
// the caller; neither a durable mutation nor a fake writer is needed.
func TestDatabaseImportNativeTransportSurvivesOrdinaryWriteDeadline(t *testing.T) {
	for _, protocol := range []string{"http1", "http2"} {
		t.Run(protocol, func(t *testing.T) {
			ready := make(chan time.Time, 1)
			h := NewHandler(Deps{
				RequireScope:         func(*gin.Context, string, ...string) bool { return true },
				RequireStepUp:        func(*gin.Context, string, string) bool { return true },
				AuthorizationChanged: func() {},
				Audit:                func(*gin.Context, string, string, string, string, map[string]any) {},
				Actor:                func(*gin.Context) string { return "synthetic-admin" },
				JSONMsg:              func(c *gin.Context, _ string, err error) { c.JSON(200, gin.H{"success": err == nil}) },
			})
			g := gin.New()
			g.POST("/importdb", func(c *gin.Context) {
				c.Request.Body = &transferBodyBarrier{ReadCloser: c.Request.Body, ready: ready, c: c}
				h.ImportDb(c)
			})
			s := httptest.NewUnstartedServer(g)
			const ordinaryWindow = 50 * time.Millisecond
			s.Config.WriteTimeout = ordinaryWindow
			if protocol == "http2" {
				s.EnableHTTP2 = true
				s.StartTLS()
			} else {
				s.Start()
			}
			t.Cleanup(s.Close)
			body, sender := io.Pipe()
			t.Cleanup(func() { _ = body.Close(); _ = sender.Close() })
			form := multipart.NewWriter(sender)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.URL+"/importdb", body)
			request.Header.Set("Content-Type", form.FormDataContentType())
			result := make(chan error, 1)
			go func() {
				response, err := s.Client().Do(request)
				if err == nil {
					defer response.Body.Close()
					var dto struct {
						Success bool `json:"success"`
					}
					err = json.NewDecoder(response.Body).Decode(&dto)
					if err == nil && (response.StatusCode != 200 || dto.Success || (protocol == "http2" && response.ProtoMajor != 2)) {
						err = io.ErrUnexpectedEOF
					}
				}
				result <- err
			}()
			select {
			case deadline := <-ready:
				if !deadline.IsZero() && (time.Until(deadline) <= 0 || time.Until(deadline) > databaseTransferTimeout) {
					t.Fatal("invalid owner deadline")
				}
			case <-ctx.Done():
				t.Fatal("import did not reach multipart body")
			}
			// Cross the transport's actual original timeout, after the handler
			// barrier proves that authentication and timeout selection finished.
			select {
			case <-time.After(2 * ordinaryWindow):
			case <-ctx.Done():
				t.Fatal("fixture deadline")
			}
			part, err := form.CreateFormFile("db", "invalid.db")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(part, strings.NewReader("not a SQLite archive")); err != nil {
				t.Fatal(err)
			}
			if err := form.Close(); err != nil {
				t.Fatal(err)
			}
			_ = sender.Close()
			select {
			case err := <-result:
				if err != nil {
					t.Fatalf("lost import result at ordinary transport deadline: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("bounded import response not received")
			}
		})
	}
}

type deadlineWriter struct {
	*httptest.ResponseRecorder
	read, write time.Time
}

func (w *deadlineWriter) SetReadDeadline(t time.Time) error  { w.read = t; return nil }
func (w *deadlineWriter) SetWriteDeadline(t time.Time) error { w.write = t; return nil }

func TestDatabaseTransferDeadlinesRemainBehindAuthorization(t *testing.T) {
	for _, boundary := range []string{"scope", "step-up", "authorization-changed"} {
		t.Run(boundary, func(t *testing.T) {
			w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/importdb", nil)
			h := NewHandler(Deps{RequireScope: func(c *gin.Context, _ string, _ ...string) bool {
				if boundary == "scope" {
					c.AbortWithStatus(403)
					return false
				}
				return true
			}, RequireStepUp: func(*gin.Context, string, string) bool { return true }, AuthorizationChanged: func() {}, EnforceStepUpHeader: true})
			if boundary == "authorization-changed" {
				h.AuthorizationChanged = nil
				c.Request.Header.Set("X-Step-Up-Token", "synthetic")
			}
			h.ImportDb(c)
			if !w.read.IsZero() || !w.write.IsZero() {
				t.Fatal("unauthorized/unavailable request gained extended transport deadline")
			}
		})
	}
}

func TestDatabaseTransferPreservesEarlierCancellation(t *testing.T) {
	w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)
	deadline := time.Now().Add(time.Second)
	parent, stop := context.WithDeadline(t.Context(), deadline)
	defer stop()
	c.Request = httptest.NewRequest("POST", "/importdb", nil).WithContext(parent)
	cancel := beginDatabaseTransfer(c)
	defer cancel()
	got, _ := c.Request.Context().Deadline()
	if !got.Equal(deadline) || !w.read.Equal(deadline) || !w.write.Equal(deadline.Add(10*time.Second)) {
		t.Fatal("caller deadline overridden or response window unbounded")
	}
	stop()
	if c.Request.Context().Err() != context.Canceled {
		t.Fatal("caller cancellation detached")
	}
}
