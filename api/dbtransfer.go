package api

import dbtransferhttp "github.com/MalenkiySolovey/solovey-ui/api/dbtransfer"

func (a *ApiService) dbTransferHandler() *dbtransferhttp.Handler {
	return dbtransferhttp.NewHandler(a.dbTransferDeps())
}

func (a *ApiService) dbTransferDeps() dbtransferhttp.Deps {
	return dbtransferhttp.Deps{
		SettingService:  a.SettingService,
		TelegramService: a.TelegramService,
		RequireScope:    a.requireTokenScopeAny,
		Audit:           a.recordAudit,
		Actor:           requestActor,
		RemoteIP:        getRemoteIp,
		JSONMsg:         jsonMsg,
	}
}
