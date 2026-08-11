package api

import (
	"github.com/MalenkiySolovey/solovey-ui/service"
	datalifecycleservice "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
	sshmanagementservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
)

type ApiService struct {
	Runtime            *service.Runtime
	RestartScheduler   service.RestartScheduler
	SettingService     service.SettingService
	UserService        service.UserService
	ConfigService      *service.ConfigService
	ClientService      service.ClientService
	TlsService         service.TlsService
	InboundService     service.InboundService
	OutboundService    service.OutboundService
	EndpointService    service.EndpointService
	ServicesService    service.ServicesService
	StatsService       service.StatsService
	ServerService      service.ServerService
	AuditService       service.AuditService
	DiagnosticsService service.DiagnosticsService
	VersionService     service.VersionService
	DoctorService      service.DoctorService
	Deployment         *deploymentservice.Manager
	SSHManagement      *sshmanagementservice.Manager
	Update             *updateservice.LifecycleManager
	DataLifecycle      *datalifecycleservice.Manager
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
	a.ConfigService = service.NewConfigServiceWithRuntime(runtime)
	a.ClientService = service.ClientService{Runtime: runtime}
	a.TlsService = service.TlsService{
		Runtime:         runtime,
		InboundService:  service.InboundService{Runtime: runtime, ClientService: service.ClientService{Runtime: runtime}},
		ServicesService: service.ServicesService{Runtime: runtime},
	}
	a.InboundService = service.InboundService{Runtime: runtime, ClientService: service.ClientService{Runtime: runtime}}
	a.ServicesService = service.ServicesService{Runtime: runtime}
	a.RestartScheduler = runtime.RestartScheduler()
	a.StatsService = service.StatsService{Runtime: runtime}
	a.ServerService = service.NewServerService(runtime)
	a.AuditService = service.AuditService{Runtime: runtime}
	a.DiagnosticsService = service.DiagnosticsService{Runtime: runtime}
	a.DoctorService = service.DoctorService{Runtime: runtime}
	a.Deployment = deploymentservice.Shared()
	a.SSHManagement = sshmanagementservice.Shared()
	a.Update = updateservice.SharedLifecycle()
	a.DataLifecycle = datalifecycleservice.Shared()
}
