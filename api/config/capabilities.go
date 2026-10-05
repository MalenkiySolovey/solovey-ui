package config

import (
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/gin-gonic/gin"
)

func (a *Handler) GetCapabilities(c *gin.Context) {
	if a.RequireScope != nil && !a.RequireScope(c, "capabilities", "admin", "read", "write") {
		return
	}
	a.JSONObj(c, entitycapabilities.Current(), nil)
}
