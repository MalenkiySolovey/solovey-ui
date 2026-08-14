package service

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"gorm.io/gorm"
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
	db := dbsqlite.DB()
	return b.BuildFromDB(db, data)
}

// BuildFromDB renders a complete candidate from the supplied database view.
// It is used by transactional workflows that must validate uncommitted rows
// without temporarily exposing them through the process-wide database handle.
func (b SingBoxConfigBuilder) BuildFromDB(db *gorm.DB, data string) ([]byte, error) {
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	if len(data) == 0 {
		var setting model.Setting
		result := db.Model(&model.Setting{}).Where("key = ?", "config").Limit(1).Find(&setting)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			data = defaultSingBoxBaseConfig
		} else {
			data = setting.Value
		}
	}
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
