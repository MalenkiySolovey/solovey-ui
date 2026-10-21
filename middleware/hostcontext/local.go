// Package hostcontext binds an explicit HostRef to API requests. Legacy clients
// without a Host header retain Local behavior. No remote transport is provided.
package hostcontext

import (
	"context"
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	"github.com/gin-gonic/gin"
)

const Header = "X-Solovey-Host"

type contextKey struct{}

func FromContext(ctx context.Context) (hostref.Ref, bool) {
	if ctx == nil {
		return hostref.Ref{}, false
	}
	host, ok := ctx.Value(contextKey{}).(hostref.Ref)
	return host, ok && host.IsLocal()
}

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values(Header)
		if len(values) > 1 || (len(values) == 1 && values[0] != hostref.Local().ID) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"success": false, "msg": "Host is unavailable; only Local is supported", "obj": nil})
			return
		}
		host := hostref.Local()
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), contextKey{}, host))
		c.Header(Header, host.ID)
		c.Next()
	}
}
