package api

import (
	"github.com/MalenkiySolovey/solovey-ui/backend/http/securityroutes"
	"github.com/gin-gonic/gin"
)

func strictQueryKeys(c *gin.Context, message string, allowed ...string) bool {
	return securityroutes.StrictQueryKeys(c, message, allowed...)
}
