package api

import "github.com/gin-gonic/gin"

func (a *ApiService) GetDiagnosticsReport(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "diagnostics", "admin", "read", "write", "observability") {
		return
	}
	jsonObj(c, a.DiagnosticsService.Report(), nil)
}

func (a *ApiService) GetDiagnosticsBundle(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "diagnostics", "admin", "read", "write", "observability") {
		return
	}
	jsonObj(c, a.DiagnosticsService.Bundle(), nil)
}
