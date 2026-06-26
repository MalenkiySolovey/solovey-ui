package api

import (
	"github.com/MalenkiySolovey/solovey-ui/service"
	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
)

type ApiService struct {
	Runtime               *service.Runtime
	RestartScheduler      service.RestartScheduler
	SettingService        service.SettingService
	UserService           service.UserService
	ConfigService         service.ConfigService
	ClientService         service.ClientService
	TlsService            service.TlsService
	InboundService        service.InboundService
	OutboundService       service.OutboundService
	RemoteOutboundService service.RemoteOutboundService
	EndpointService       service.EndpointService
	ServicesService       service.ServicesService
	StatsService          service.StatsService
	ServerService         service.ServerService
	AuditService          service.AuditService
	ObservabilityService  service.ObservabilityService
	DiagnosticsService    service.DiagnosticsService
	TelegramService       service.TelegramService
	VersionService        service.VersionService
	DoctorService         service.DoctorService
	PanelUpdateService    *serviceupdate.Manager
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
	a.RemoteOutboundService = service.RemoteOutboundService{Runtime: runtime}
	a.ServicesService = service.ServicesService{Runtime: runtime}
	a.RestartScheduler = runtime.RestartScheduler()
	a.StatsService = service.StatsService{Runtime: runtime}
	a.ServerService = service.NewServerService(runtime)
	a.AuditService = service.AuditService{Runtime: runtime}
	a.ObservabilityService = service.ObservabilityService{
		ServerService: service.NewServerService(runtime),
	}
	a.DiagnosticsService = service.DiagnosticsService{Runtime: runtime}
	a.TelegramService = service.TelegramService{Runtime: runtime}
	a.DoctorService = service.DoctorService{Runtime: runtime}
	a.PanelUpdateService = service.NewPanelUpdateManager()
}
