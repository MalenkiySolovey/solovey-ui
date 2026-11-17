package securityroutes

import "github.com/gin-gonic/gin"

func StrictQueryKeys(c *gin.Context, message string, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range c.Request.URL.Query() {
		if _, ok := known[key]; !ok {
			BadRequest(c, message)
			return false
		}
	}
	return true
}
