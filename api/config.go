package api

import confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"

func (a *ApiService) configHandler() *confighttp.Handler {
	return confighttp.NewHandler(a.configDeps())
}

func (a *ApiService) configDeps() confighttp.Deps {
	return confighttp.Deps{
		Runtime:          a.Runtime,
		RestartScheduler: a.RestartScheduler,
		ConfigService:    a.ConfigService,
		SettingService:   a.SettingService,
		UserService:      a.UserService,
		ClientService:    a.ClientService,
		TlsService:       a.TlsService,
		InboundService:   a.InboundService,
		OutboundService:  a.OutboundService,
		EndpointService:  a.EndpointService,
		ServicesService:  a.ServicesService,
		TelegramService:  a.TelegramService,
		StatsService:     a.StatsService,
		ServerService:    a.ServerService,
		RequireScope:     a.requireTokenScopeAny,
		Actor:            requestActor,
		Hostname:         getHostname,
		JSONObj:          jsonObj,
		JSONMsg:          jsonMsg,
		Audit:            a.recordAudit,
		ValidateTarget:   confighttp.ValidateOutboundCheckTarget,
		RemoteIP:         getRemoteIp,
	}
}
