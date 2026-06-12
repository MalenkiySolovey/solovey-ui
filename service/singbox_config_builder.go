package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database"
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

	var singboxConfig map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &singboxConfig); err != nil {
		return nil, err
	}

	db := database.GetDB()
	inbounds, err := b.InboundService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}
	if err := setSingBoxConfigSection(singboxConfig, "inbounds", inbounds); err != nil {
		return nil, err
	}

	outbounds, err := b.OutboundService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}
	if err := setSingBoxConfigSection(singboxConfig, "outbounds", outbounds); err != nil {
		return nil, err
	}

	services, err := b.ServicesService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}
	if err := setSingBoxConfigSection(singboxConfig, "services", services); err != nil {
		return nil, err
	}

	endpoints, err := b.EndpointService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}
	if err := setSingBoxConfigSection(singboxConfig, "endpoints", endpoints); err != nil {
		return nil, err
	}

	return json.MarshalIndent(singboxConfig, "", "  ")
}

func setSingBoxConfigSection(config map[string]json.RawMessage, section string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	config[section] = raw
	return nil
}
