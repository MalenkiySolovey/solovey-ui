package config

import (
	subexternal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/external"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"

	"github.com/gin-gonic/gin"
)

func (a *Handler) LinkConvert(c *gin.Context) {
	link := c.Request.FormValue("link")
	result, _, err := suburi.Parse(link, 0)
	a.JSONObj(c, result, err)
}

func (a *Handler) SubConvert(c *gin.Context) {
	link := c.Request.FormValue("link")
	result, err := subexternal.FetchOutbounds(link)
	a.JSONObj(c, result, err)
}
