package config

import (
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/tlsprobe"

	"github.com/gin-gonic/gin"
)

func (a *Handler) GetCertPing(c *gin.Context) {
	server := strings.TrimSpace(c.PostForm("server"))
	if server == "" {
		server = strings.TrimSpace(c.PostForm("domain"))
	}
	result, err := tlsprobe.Probe(c.Request.Context(), tlsprobe.ProbeConfig{
		Server:     server,
		Port:       c.PostForm("port"),
		ServerName: c.PostForm("serverName"),
	})
	a.JSONObj(c, result, err)
}
