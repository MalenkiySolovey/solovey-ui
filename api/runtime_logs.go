package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/gin-gonic/gin"
)

// This is a bounded current-generation projection of the existing log owner,
// not a new log ring or reconnecting WebSocket runtime. The core callback never
// waits on a browser; a full queue terminates this request's subscription.
func (a *ApiService) streamRuntimeLogs(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "runtime_logs", "admin", "read", "write", "observability") {
		return
	}
	generation := c.Query("generation")
	if !validRuntimeGeneration(generation) {
		c.JSON(http.StatusBadRequest, Msg{Msg: "invalid_runtime_request"})
		return
	}
	core := a.runtimeCore()
	status := core.RuntimeStatus(c.Request.Context())
	if !status.APIAvailable || status.Generation != generation {
		c.JSON(http.StatusConflict, Msg{Msg: "runtime_stream_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	events := make(chan coreruntime.RuntimeLogEvent, 64)
	finished := make(chan error, 1)
	go func() {
		finished <- core.SubscribeRuntimeLogs(ctx, generation, func(event coreruntime.RuntimeLogEvent) bool {
			select {
			case events <- event:
				return true
			default:
				return false
			}
		})
	}()
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	controller := http.NewResponseController(c.Writer)
	write := func(value any) bool {
		// Gin 1.12 unwraps to the native writer. Unsupported deadlines (e.g.
		// httptest) are harmless; deployed HTTP transports support this bound.
		_ = controller.SetWriteDeadline(time.Now().Add(2 * time.Second))
		// HTTP2 owns a stream timer even while idle. Bound this write/flush,
		// then clear the deadline so the next frame can arrive within 30 seconds.
		defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
		if json.NewEncoder(c.Writer).Encode(value) != nil {
			return false
		}
		return controller.Flush() == nil
	}
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-finished:
			reason := "stream_closed"
			if err != nil {
				reason = runtimeReason(err)
			}
			write(gin.H{"generation": generation, "closed": true, "reason": reason})
			return
		case event := <-events:
			if !write(event) {
				return
			}
		}
	}
}
