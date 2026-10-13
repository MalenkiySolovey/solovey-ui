package dbtransfer

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const databaseTransferTimeout = 5 * time.Minute

// Call only after the admin scope gate. Database transfer performs bounded
// upload, validation and durable file publication before returning its result;
// the ordinary panel's 30-second write deadline must not discard that result.
// The production router excludes these exact routes from gzip so the native
// controller can set an HTTP/2 stream deadline without changing its connection.
func beginDatabaseTransfer(c *gin.Context) context.CancelFunc {
	ctx, cancel := context.WithTimeout(c.Request.Context(), databaseTransferTimeout)
	c.Request = c.Request.WithContext(ctx)
	deadline, _ := ctx.Deadline()
	controller := http.NewResponseController(c.Writer)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline.Add(10 * time.Second))
	return cancel
}
