package server

import (
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/gin-gonic/gin"
)

type FormatHandlers struct {
	JSON    gin.HandlerFunc
	Clash   gin.HandlerFunc
	Xray    gin.HandlerFunc
	Headers gin.HandlerFunc
}

func RegisterFormatRoute(engine *gin.Engine, registered map[string]string, path string, format string, handlers FormatHandlers) error {
	path = NormalizeRoutePath(path)
	if path == "/" {
		return common.NewError("subscription format path cannot be root")
	}
	if existing, ok := registered[path]; ok {
		if existing == format {
			return nil
		}
		return common.NewError("subscription path conflict: ", path)
	}
	registered[path] = format

	group := engine.Group(path)
	group.Use(RateLimitMiddleware())
	switch format {
	case "json":
		if handlers.JSON == nil || handlers.Headers == nil {
			return common.NewError("subscription json handlers are not configured")
		}
		group.GET("/:subid", handlers.JSON)
		group.HEAD("/:subid", formatHeaders(format, handlers.Headers))
	case "clash":
		if handlers.Clash == nil || handlers.Headers == nil {
			return common.NewError("subscription clash handlers are not configured")
		}
		group.GET("/:subid", handlers.Clash)
		group.HEAD("/:subid", formatHeaders(format, handlers.Headers))
	case "xray":
		if handlers.Xray == nil || handlers.Headers == nil {
			return common.NewError("subscription xray handlers are not configured")
		}
		group.GET("/:subid", handlers.Xray)
		group.HEAD("/:subid", formatHeaders(format, handlers.Headers))
	default:
		return common.NewError("unknown subscription format: ", format)
	}
	return nil
}

func formatHeaders(format string, handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("subscriptionFormat", format)
		handler(c)
	}
}

func RememberPath(registered map[string]string, path string, format string) error {
	path = NormalizeRoutePath(path)
	if existing, ok := registered[path]; ok && existing != format {
		return common.NewError("subscription path conflict: ", path)
	}
	registered[path] = format
	return nil
}

func JoinPath(base string, child string) string {
	if base == "/" {
		return NormalizeRoutePath(child)
	}
	return NormalizeRoutePath(strings.TrimRight(base, "/") + "/" + strings.Trim(child, "/"))
}

func NormalizeRoutePath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}
