package api

import "github.com/gin-gonic/gin"

func strictQueryKeys(c *gin.Context, message string, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range c.Request.URL.Query() {
		if _, ok := known[key]; !ok {
			securityBadRequest(c, message)
			return false
		}
	}
	return true
}
