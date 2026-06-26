package telemetry

import "github.com/gin-gonic/gin"

func (a *Handler) GetDiagnosticsReport(c *gin.Context) {
	if !a.RequireScope(c, "diagnostics", "admin", "read", "write", "observability") {
		return
	}
	a.JSONObj(c, a.DiagnosticsService.Report(), nil)
}

func (a *Handler) GetDiagnosticsBundle(c *gin.Context) {
	if !a.RequireScope(c, "diagnostics", "admin", "read", "write", "observability") {
		return
	}
	a.JSONObj(c, a.DiagnosticsService.Bundle(), nil)
}
