package api

import importxuihttp "github.com/MalenkiySolovey/solovey-ui/api/importxui"

func (a *ApiService) importXUIHandler() *importxuihttp.Handler {
	return importxuihttp.NewHandler(a.importXUIDeps())
}

func (a *ApiService) importXUIDeps() importxuihttp.Deps {
	return importxuihttp.Deps{
		AuditService: a.AuditService,
		RequireScope: a.requireTokenScopeAny,
		Audit:        a.recordAudit,
		Actor:        requestActor,
		RemoteIP:     getRemoteIp,
		Hostname:     getHostname,
		JSONObj:      jsonObj,
		JSONMsg:      jsonMsg,
	}
}
