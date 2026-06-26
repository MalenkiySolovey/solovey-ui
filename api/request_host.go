package api

import (
	"net"
	"strings"

	"github.com/gin-gonic/gin"
)

func getHostname(c *gin.Context) string {
	host := c.Request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host
}
