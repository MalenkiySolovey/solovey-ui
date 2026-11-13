//go:build !minimal

package service

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	feature "github.com/MalenkiySolovey/solovey-component-fallback-html/service"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Runtime composes the portable runtime with the existing Backend HTTP,
// registry, trusted client identity and request-limit owners.
type Runtime struct {
	*feature.Runtime
	serving HTTPServing
	surface *backendPublicSurface
	limiter *ratelimit.FixedWindow[string]
}

var DefaultRuntime = NewRuntime()

const runtimeDrainTimeout = 5 * time.Second

func NewRuntime() *Runtime {
	r := &Runtime{limiter: ratelimit.NewFixedWindow[string](time.Minute, 240, 8192, 0)}
	serving := &backendHTTP{listeners: map[string]*runtimeListener{}}
	r.surface = &backendPublicSurface{}
	r.Runtime = feature.NewRuntime(serving, r.surface, backendRequestLimiter{runtime: r})
	r.serving = serving
	serving.handler = r.Runtime
	return r
}
func (r *Runtime) Start(db *gorm.DB) error { return r.Runtime.Start(backendStoreFor(db, r)) }
func (r *Runtime) StartContext(ctx context.Context, db *gorm.DB) error {
	return r.Runtime.StartContext(ctx, backendStoreFor(db, r))
}
func (r *Runtime) Rebuild(db *gorm.DB) error { return r.Runtime.Rebuild(backendStoreFor(db, r)) }
func (r *Runtime) RebuildContext(ctx context.Context, db *gorm.DB) error {
	return r.Runtime.RebuildContext(ctx, backendStoreFor(db, r))
}
func (r *Runtime) serveHTTPPublic(w http.ResponseWriter, req *http.Request) {
	r.Runtime.ServeHTTP(w, req)
}
func (r *Runtime) ServePublic(c *gin.Context, ctx publicsurface.Context) bool {
	return serveBackendPublic(c, ctx, r.Runtime.ServePublic)
}
func serveBackendPublic(c *gin.Context, ctx publicsurface.Context, handle feature.PublicHandler) bool {
	handled := handle(c.Writer, c.Request, ctx, c.ClientIP())
	if handled {
		c.Writer.WriteHeaderNow()
		if c.Writer.Status() == http.StatusTooManyRequests || c.Writer.Status() == http.StatusNotModified {
			c.Abort()
		}
	}
	return handled
}

type backendPublicSurface struct{ registered atomic.Bool }
type backendPublicHandler struct{ handle feature.PublicHandler }

func (h backendPublicHandler) ServePublic(c *gin.Context, ctx publicsurface.Context) bool {
	return serveBackendPublic(c, ctx, h.handle)
}
func (s *backendPublicSurface) RegisterPublic(handle feature.PublicHandler) (func(), error) {
	unregister, err := publicsurface.Register("fallback-html", backendPublicHandler{handle})
	if err != nil {
		return nil, err
	}
	s.registered.Store(true)
	return func() { unregister(); s.registered.Store(false) }, nil
}
func (*backendPublicSurface) ObservationsEnabled() bool {
	return publicsurface.ObservationSubscribers() > 0
}
func (*backendPublicSurface) Observe(o publicsurface.Observation) { publicsurface.EmitObservation(o) }

type backendRequestLimiter struct{ runtime *Runtime }

func (b backendRequestLimiter) Allow(key string) feature.RequestLimitDecision {
	if b.runtime.limiter == nil {
		return feature.RequestLimitDecision{Allowed: true}
	}
	d := b.runtime.limiter.Allow(key)
	return feature.RequestLimitDecision{Allowed: d.Allowed, RetryAfter: d.RetryAfter}
}
func (b backendRequestLimiter) ResetAll() {
	if b.runtime.limiter != nil {
		b.runtime.limiter.ResetAll()
	}
}

var runtimeTargetKey = feature.RuntimeTargetKey
var recordEvent = feature.RecordEvent

func uintString(value uint) string { return strconv.FormatUint(uint64(value), 10) }
func acquireRuntimeLock(ctx context.Context, mu *sync.Mutex) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for !mu.TryLock() {
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		mu.Unlock()
		return err
	}
	return nil
}
