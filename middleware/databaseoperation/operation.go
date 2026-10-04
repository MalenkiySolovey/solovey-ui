// Package databaseoperation owns the HTTP adapter for finite SQLite operation
// admission. The SQLite owner supplies all generation and maintenance policy.
package databaseoperation

import (
	"net/http"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
)

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, release, err := dbsqlite.AcquireOperation(c.Request.Context())
		if err != nil {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "Database maintenance is in progress"})
			return
		}
		defer release()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
