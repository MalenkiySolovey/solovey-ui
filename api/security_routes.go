package api

import (
	"github.com/MalenkiySolovey/solovey-ui/backend/http/response"
	"github.com/MalenkiySolovey/solovey-ui/backend/http/securityroutes"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func newSecurityHTTP(a *APIHandler) *securityroutes.Handler {
	if a == nil {
		a = &APIHandler{}
	}
	return &securityroutes.Handler{
		Settings: &a.SettingService, Users: &a.UserService,
		MFA:      &service.MFAService{SettingService: a.SettingService},
		StepUp:   &service.StepUpService{SettingService: a.SettingService},
		Sessions: &service.SecuritySessionService{}, Browser: browserSecurity(&a.SettingService),
		Response: response.Writer{Warning: logger.Warning},
		Audit:    a.recordAudit, AuditSynchronous: a.recordAuditSynchronous,
		NotifyEvent: service.NotifyPanelEvent, ReloadTokens: a.reloadTokens,
		ComponentUIReadPermissions: componentUIReadPermissions,
		IsBearer:                   func(c *gin.Context) bool { _, bearer := requestTokenScope(c); return bearer },
	}
}
func (a *APIHandler) registerSecurityRoutes(g *gin.RouterGroup) { newSecurityHTTP(a).RegisterRoutes(g) }
func (a *APIHandler) requireStepUpAction(c *gin.Context, operation, target string) bool {
	return newSecurityHTTP(a).RequireStepUpAction(c, operation, target)
}
func requireAuthenticatedSecurityContext(c *gin.Context) (SessionSecurityContext, bool) {
	return newSecurityHTTP(nil).RequireAuthenticatedContext(c)
}
func decodeSecurityJSON(c *gin.Context, target any) bool { return securityroutes.DecodeJSON(c, target) }
func securityBadRequest(c *gin.Context, msg string)      { securityroutes.BadRequest(c, msg) }
func validDigest(value string) bool                      { return securityroutes.ValidDigest(value) }
func validStepUpOperation(value string) bool             { return securityroutes.ValidStepUpOperation(value) }
func securityTargetDigest(value string) string           { return securityroutes.TargetDigest(value) }

const stepUpHeader = securityroutes.StepUpHeader

type issueStepUpRequest = securityroutes.IssueStepUpRequest

func realtimeSessionBinding(c *gin.Context) (service.RealtimeSessionBinding, bool) {
	return newSecurityHTTP(nil).RealtimeSessionBinding(c)
}
func realtimeSessionValid(binding service.RealtimeSessionBinding) bool {
	return securityroutes.RealtimeSessionValid(binding)
}
