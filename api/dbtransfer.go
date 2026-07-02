package api

import (
	dbtransferhttp "github.com/MalenkiySolovey/solovey-ui/api/dbtransfer"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func (a *ApiService) dbTransferHandler() *dbtransferhttp.Handler {
	return dbtransferhttp.NewHandler(a.dbTransferDeps())
}

func (a *ApiService) dbTransferDeps() dbtransferhttp.Deps {
	return dbtransferhttp.Deps{
		SettingService: a.SettingService,
		NotifyEvent:    service.NotifyPanelEvent,
		RequireScope:   a.requireTokenScopeAny,
		Audit:          a.recordAudit,
		Actor:          requestActor,
		RemoteIP:       getRemoteIp,
		JSONMsg:        jsonMsg,
	}
}
