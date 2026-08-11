package api

import (
	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func (a *ApiService) configHandler() *confighttp.Handler {
	return confighttp.NewHandler(a.configDeps())
}

func (a *ApiService) configDeps() confighttp.Deps {
	configService := a.ConfigService
	if configService == nil {
		configService = service.NewConfigServiceWithRuntime(a.Runtime)
		a.ConfigService = configService
	}
	return confighttp.Deps{
		Runtime:          a.Runtime,
		RestartScheduler: a.RestartScheduler,
		ConfigService:    configService,
		SettingService:   a.SettingService,
		UserService:      a.UserService,
		ClientService:    a.ClientService,
		TlsService:       a.TlsService,
		InboundService:   a.InboundService,
		OutboundService:  a.OutboundService,
		EndpointService:  a.EndpointService,
		ServicesService:  a.ServicesService,
		StatsService:     a.StatsService,
		ServerService:    a.ServerService,
		NotifyEvent:      service.NotifyPanelEvent,
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
