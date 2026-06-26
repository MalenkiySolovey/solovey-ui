package api

import (
	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
	telemetryhttp "github.com/MalenkiySolovey/solovey-ui/api/telemetry"
)

func (a *ApiService) telemetryHandler() *telemetryhttp.Handler {
	return telemetryhttp.NewHandler(a.telemetryDeps())
}

func (a *ApiService) telemetryDeps() telemetryhttp.Deps {
	return telemetryhttp.Deps{
		StatsService:           a.StatsService,
		ServerService:          a.ServerService,
		DiagnosticsService:     a.DiagnosticsService,
		DoctorService:          a.DoctorService,
		ObservabilityService:   a.ObservabilityService,
		AuditService:           a.AuditService,
		VersionService:         a.VersionService,
		RequireScope:           a.requireTokenScopeAny,
		JSONObj:                jsonObj,
		JSONMsg:                jsonMsg,
		Hostname:               getHostname,
		ValidateTarget:         confighttp.ValidateOutboundCheckTarget,
		LoginUser:              GetLoginUser,
		RequireAuditAdminScope: a.requireAuditAdminScope,
		Actor:                  requestActor,
		RemoteIP:               getRemoteIp,
		CheckAuditRateLimit:    checkAuditEndpointRateLimit,
		AuditRateLimitKey:      auditEndpointRateLimitKey,
		AuditRateLimitWindow:   auditEndpointRateLimitWindow,
		Audit:                  a.recordAudit,
	}
}
