package api

import "github.com/gin-gonic/gin"

func routeExists(router *gin.Engine, method string, path string) bool {
	for _, route := range router.Routes() {
		if route.Method == method && route.Path == path {
			return true
		}
	}
	return false
}
