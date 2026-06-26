package service

import (
	"encoding/json"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
)

type SingBoxConfigBuilder struct {
	SettingService  SettingService
	InboundService  InboundService
	OutboundService OutboundService
	ServicesService ServicesService
	EndpointService EndpointService
}

func NewSingBoxConfigBuilder(runtime *Runtime) SingBoxConfigBuilder {
	runtime = runtimeOrDefault(runtime)
	return SingBoxConfigBuilder{
		SettingService:  SettingService{},
		InboundService:  InboundService{Runtime: runtime, ClientService: ClientService{Runtime: runtime}},
		OutboundService: OutboundService{},
		ServicesService: ServicesService{Runtime: runtime},
		EndpointService: EndpointService{},
	}
}

func (b SingBoxConfigBuilder) Build(data string) ([]byte, error) {
	var err error
	if len(data) == 0 {
		data, err = b.SettingService.GetConfig()
		if err != nil {
			return nil, err
		}
	}

	db := dbsqlite.DB()
	inbounds, err := b.InboundService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}

	outbounds, err := b.OutboundService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}

	services, err := b.ServicesService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}

	endpoints, err := b.EndpointService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}

	return singboxconfig.BuildRuntimeConfig(json.RawMessage(data), singboxconfig.RuntimeSections{
		Inbounds:  inbounds,
		Outbounds: outbounds,
		Services:  services,
		Endpoints: endpoints,
	})
}
