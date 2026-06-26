package telemetry

import "github.com/gin-gonic/gin"

func (a *Handler) GetVersionInfo(c *gin.Context) {
	a.JSONObj(c, a.VersionService.GetVersionInfo(), nil)
}
