package api

import "github.com/MalenkiySolovey/solovey-ui/service"

type ApiService struct {
	Runtime *service.Runtime
	service.SettingService
	service.UserService
	service.ConfigService
	service.ClientService
	service.TlsService
	service.InboundService
	service.OutboundService
	service.EndpointService
	service.ServicesService
	service.PanelService
	service.StatsService
	service.ServerService
	service.AuditService
	service.ObservabilityService
	service.TelegramService
	service.VersionService
}

type Option func(*ApiService)

func WithRuntime(runtime *service.Runtime) Option {
	return func(a *ApiService) {
		if runtime != nil {
			a.Runtime = runtime
		}
	}
}

func NewApiService(options ...Option) ApiService {
	a := ApiService{
		Runtime: service.DefaultRuntime(),
	}
	for _, option := range options {
		if option != nil {
			option(&a)
		}
	}
	a.bindRuntime()
	return a
}

func (a *ApiService) bindRuntime() {
	runtime := a.Runtime
	if runtime == nil {
		runtime = service.DefaultRuntime()
		a.Runtime = runtime
	}
	a.UserService = service.UserService{Runtime: runtime}
	a.ConfigService = *service.NewConfigServiceWithRuntime(runtime)
	a.ClientService = service.ClientService{Runtime: runtime}
	a.TlsService = service.TlsService{
		Runtime:         runtime,
		InboundService:  service.InboundService{Runtime: runtime, ClientService: service.ClientService{Runtime: runtime}},
		ServicesService: service.ServicesService{Runtime: runtime},
	}
	a.InboundService = service.InboundService{Runtime: runtime, ClientService: service.ClientService{Runtime: runtime}}
	a.ServicesService = service.ServicesService{Runtime: runtime}
	a.PanelService = service.PanelService{Runtime: runtime}
	a.StatsService = service.StatsService{Runtime: runtime}
	a.ServerService = service.ServerService{Runtime: runtime}
	a.AuditService = service.AuditService{Runtime: runtime}
	a.ObservabilityService = service.ObservabilityService{
		ServerService: service.ServerService{Runtime: runtime},
	}
	a.TelegramService = service.TelegramService{Runtime: runtime}
}
