//go:build !minimal

package api

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectioninterception "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/interception"
	protectionlocalproxy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/localproxy"
	protectionnativefallback "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/nativefallback"
	protectionobservation "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/observation"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionudpguard "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/udpguard"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"github.com/gin-gonic/gin"
)

const (
	readScope  = "server-protection:read"
	writeScope = "server-protection:write"
	applyScope = "server-protection:apply"
)

type Deps struct {
	Repository        *protectionrepository.Repository
	RequireScope      func(*gin.Context, string, ...string) bool
	Actor             func(*gin.Context) string
	Audit             func(*gin.Context, string, string, string, string, map[string]any)
	JSONObj           func(*gin.Context, interface{}, error)
	JSONMsg           func(*gin.Context, string, error)
	ObservationStatus func() protectionobservation.Status
	Operations        operationService
	Firewall          *protectionfirewall.Workflow
	Baseline          *protectionfirewall.BaselineService
	UDPGuard          protectionudpguard.Service
	Fronting          protectionfronting.FrontingController
	FrontingV2        frontingSemanticService
	NativeFallback    nativeFallbackService
	LocalProxy        localProxyService
	Interception      interceptionService
}

type interceptionService interface {
	Status(context.Context) (protectioninterception.StatusV1, error)
	Preview(context.Context, protectioninterception.PreviewRequestV1) (protectioninterception.PlanV1, error)
	BlockedMutation(protectioninterception.BlockedMutationRequestV1) error
	Operation(string) (protectioninterception.OperationStatusV1, error)
}

type localProxyService interface {
	Status(context.Context, bool) (protectionlocalproxy.StatusV1, error)
	Preview(context.Context, protectionlocalproxy.PlanReferenceV1) (protectionlocalproxy.PlanV1, error)
	Prepare(context.Context, string, protectionlocalproxy.PrepareRequestV1) (protectionlocalproxy.ResultV1, error)
	Apply(context.Context, protectionlocalproxy.ApplyRequestV1) (protectionlocalproxy.ResultV1, error)
	Disable(context.Context, protectionlocalproxy.DisableRequestV1) (protectionlocalproxy.ResultV1, error)
	Operation(context.Context, string) (protectionrepository.OperationLockModel, error)
	Recovery(context.Context, string) (protectionlocalproxy.RecoveryStatusV1, error)
}

type nativeFallbackService interface {
	Inspect(context.Context, uint) (coreinboundcontrol.CoreRuntimeIdentityV1, coreinboundcontrol.InboundFallbackSnapshotV1, error)
	Preview(context.Context, protectionnativefallback.PlanRequestV1) (domain.NativeFallbackPlanV1, error)
	Prepare(context.Context, protectionnativefallback.PrepareWorkflowRequestV1) (protectionnativefallback.WorkflowResultV1, error)
	Apply(context.Context, protectionnativefallback.ApplyWorkflowRequestV1) (protectionnativefallback.WorkflowResultV1, error)
	Rollback(context.Context, protectionnativefallback.RollbackWorkflowRequestV1) (protectionnativefallback.WorkflowResultV1, error)
}

type operationService interface {
	List(context.Context) ([]protectionrepository.OperationLockModel, error)
	ForceUnlock(context.Context, protectionoperations.ForceUnlockRequest) (protectionrepository.OperationLockModel, error)
	Prepare(context.Context, protectionoperations.PrepareRequest) (protectionoperations.AcquireResult, error)
	ConfirmUnavailableAction(context.Context, protectionoperations.ConfirmActionRequest) error
	ForgetState(context.Context, protectionoperations.ForgetStateRequest) (protectionrepository.OperationLockModel, error)
}

type Handler struct{ deps Deps }

func RegisterRoutes(group *gin.RouterGroup, deps Deps) {
	if deps.UDPGuard == nil {
		deps.UDPGuard = &protectionudpguard.Controller{}
	}
	if deps.LocalProxy == nil {
		deps.LocalProxy = &protectionlocalproxy.Controller{}
	}
	if deps.Interception == nil {
		deps.Interception = protectioninterception.New()
	}
	handler := Handler{deps: deps}
	routes := group.Group("/components/server-protection")
	routes.GET("/status", handler.status)
	routes.GET("/settings", handler.settings)
	routes.PUT("/settings", handler.updateSettings)
	routes.GET("/resources", handler.resources)
	routes.GET("/host-surfaces", handler.hostSurfaces)
	routes.GET("/target-capabilities", handler.targetCapabilities)
	routes.GET("/native-fallback/status", handler.nativeFallbackStatus)
	routes.GET("/udp/status", handler.udpStatus)
	routes.POST("/udp/preview", handler.udpPreview)
	routes.POST("/udp/prepare", handler.udpPrepare)
	routes.POST("/udp/apply", handler.udpApply)
	routes.POST("/udp/rollback", handler.udpRollback)
	routes.GET("/udp/operations/:operationId", handler.udpOperation)
	routes.GET("/udp/operations/:operationId/recovery", handler.udpRecovery)
	routes.GET("/local-proxy/status", handler.localProxyStatus)
	routes.POST("/local-proxy/preview", handler.localProxyPreview)
	routes.POST("/local-proxy/prepare", handler.localProxyPrepare)
	routes.POST("/local-proxy/apply", handler.localProxyApply)
	routes.POST("/local-proxy/disable", handler.localProxyDisable)
	routes.POST("/local-proxy/rollback", handler.localProxyDisable)
	routes.GET("/local-proxy/operations/:operationId", handler.localProxyOperation)
	routes.GET("/local-proxy/operations/:operationId/recovery", handler.localProxyRecovery)
	routes.GET("/interception/status", handler.interceptionStatus)
	routes.POST("/interception/preview", handler.interceptionPreview)
	routes.POST("/interception/prepare", handler.interceptionBlockedMutation)
	routes.POST("/interception/apply", handler.interceptionBlockedMutation)
	routes.POST("/interception/disable", handler.interceptionBlockedMutation)
	routes.POST("/interception/rollback", handler.interceptionBlockedMutation)
	routes.GET("/interception/operations/:operationId", handler.interceptionOperation)
	routes.GET("/interception/operations/:operationId/recovery", handler.interceptionOperation)
	routes.POST("/native-fallback/preview", handler.nativeFallbackPreview)
	routes.POST("/native-fallback/prepare", handler.nativeFallbackPrepare)
	routes.POST("/native-fallback/apply", handler.nativeFallbackApply)
	routes.POST("/native-fallback/rollback", handler.nativeFallbackRollback)
	routes.GET("/signals", handler.signalsV2)
	routes.GET("/decisions", handler.decisionsV2)
	routes.GET("/posture", handler.posture)
	routes.GET("/firewall-baseline", handler.firewallBaselinePlan)
	routes.POST("/decisions/resolve-preview", handler.decisionResolvePreview)
	routes.GET("/profiles", handler.profiles)
	routes.POST("/profiles", handler.createProfile)
	routes.PUT("/profiles/:id", handler.updateProfile)
	routes.DELETE("/profiles/:id", handler.deleteProfile)
	routes.POST("/profiles/:id/reattach", handler.reattachProfile)
	routes.GET("/events", handler.events)
	routes.DELETE("/events", handler.clearEvents)
	routes.GET("/graylist", handler.graylist)
	routes.DELETE("/graylist", handler.clearGraylist)
	routes.GET("/allowlist/ports", handler.portAllowlist)
	routes.POST("/allowlist/ports", handler.createPortAllowlist)
	routes.DELETE("/allowlist/ports/:id", handler.deletePortAllowlist)
	routes.GET("/allowlist/ips", handler.ipAllowlist)
	routes.POST("/allowlist/ips", handler.createIPAllowlist)
	routes.DELETE("/allowlist/ips/:id", handler.deleteIPAllowlist)
	routes.GET("/diagnostics", handler.diagnostics)
	routes.GET("/fronting/status", handler.frontingStatus)
	routes.POST("/fronting/preview", handler.frontingPreview)
	routes.POST("/fronting/prepare", handler.frontingPrepare)
	routes.POST("/fronting/sync", handler.frontingRetiredWrite)
	routes.POST("/fronting/apply", handler.frontingApply)
	routes.POST("/fronting/rollback", handler.frontingRollback)
	routes.GET("/fronting/operations/:operationId", handler.frontingOperation)
	routes.GET("/fronting/operations/:operationId/recovery", handler.frontingRecovery)
	routes.POST("/firewall/preview", handler.firewallPreview)
	routes.POST("/firewall/prepare", handler.firewallPrepare)
	routes.GET("/operations", handler.operations)
	routes.POST("/operations/:operationId/force-unlock", handler.forceUnlock)
	routes.POST("/operations/:operationId/forget-state", handler.forgetState)
	routes.POST("/firewall/apply", handler.firewallApply)
	routes.POST("/firewall/rollback", handler.firewallRollback)
	routes.POST("/ports/prepare", handler.prepareOperation)
	routes.POST("/ports/apply", handler.confirmUnavailable("apply"))
	routes.POST("/ports/rollback", handler.confirmUnavailable("rollback"))
}
