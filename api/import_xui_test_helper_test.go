//go:build !minimal

package api

import (
	importxuihttp "github.com/MalenkiySolovey/solovey-ui/components/import-xui/api"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func (a *ApiService) importXUIHandler() *importxuihttp.Handler {
	return importxuihttp.NewHandler(importxuihttp.Deps{
		AuditService: service.AuditService{Runtime: a.Runtime},
		RequireScope: a.requireTokenScopeAny,
		Audit:        a.recordAudit,
		Actor:        requestActor,
		RemoteIP:     getRemoteIp,
		Hostname:     getHostname,
		JSONObj:      jsonObj,
		JSONMsg:      jsonMsg,
	})
}
