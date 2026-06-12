package api

import "github.com/gin-gonic/gin"

func (a *ApiService) GetVersionInfo(c *gin.Context) {
	jsonObj(c, a.VersionService.GetVersionInfo(), nil)
}
