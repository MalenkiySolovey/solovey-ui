package publicsurface

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// Context describes the managed web listener surface that public handlers may
// use to avoid stealing admin, API, asset, ACME, or subscription paths.
type Context struct {
	AdminBasePath string
}

// Handler serves a public NoRoute request. It returns true when the request was
// handled and the web server must not continue to the admin fallback.
type Handler interface {
	ServePublic(*gin.Context, Context) bool
}

type handlerEntry struct {
	owner   string
	handler Handler
}

const maxPublicSurfaceHandlers = 64

var registry = struct {
	sync.Mutex
	entries  []handlerEntry
	snapshot atomic.Value // []handlerEntry
}{}

func init() {
	registry.snapshot.Store([]handlerEntry(nil))
}

// Register installs a public surface handler and returns an unregister
// function. Owner is only a registry key; the web package does not interpret it.
func Register(owner string, handler Handler) func() {
	if owner == "" || handler == nil {
		panic("public surface owner and handler are required")
	}
	registry.Lock()
	for _, entry := range registry.entries {
		if entry.owner == owner {
			registry.Unlock()
			panic("public surface owner is already registered: " + owner)
		}
	}
	if len(registry.entries) >= maxPublicSurfaceHandlers {
		registry.Unlock()
		panic("public surface handler capacity exceeded")
	}
	registry.entries = append(registry.entries, handlerEntry{owner: owner, handler: handler})
	storeSnapshotLocked()
	registry.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			registry.Lock()
			registry.entries = removeOwner(registry.entries, owner)
			storeSnapshotLocked()
			registry.Unlock()
		})
	}
}

// Serve asks the current immutable handler snapshot to handle the NoRoute
// request. The hot path does not touch the mutable registry.
func Serve(c *gin.Context, ctx Context) bool {
	entries, _ := registry.snapshot.Load().([]handlerEntry)
	for _, entry := range entries {
		if entry.handler.ServePublic(c, ctx) {
			return true
		}
	}
	return false
}

// Handled404 emits a deliberately plain fallback response when no public
// handler is active.
func Handled404(c *gin.Context) {
	c.String(http.StatusNotFound, "")
}

func removeOwner(entries []handlerEntry, owner string) []handlerEntry {
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.owner != owner {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func storeSnapshotLocked() {
	snapshot := append([]handlerEntry(nil), registry.entries...)
	registry.snapshot.Store(snapshot)
}
