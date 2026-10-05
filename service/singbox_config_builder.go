package service

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	runtimeprojection "github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
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
type RuntimeProjection struct {
	Config      []byte
	Eligibility runtimeprojection.Report
}

func (b SingBoxConfigBuilder) BuildFromDB(db *gorm.DB, data string) ([]byte, error) {
	projection, err := b.BuildProjectionFromDB(db, data)
	return projection.Config, err
}

// BuildProjectionFromDB provides explicit omission diagnostics and validates
// all references against the candidate view before rule-set preparation.
func (b SingBoxConfigBuilder) BuildProjectionFromDB(db *gorm.DB, data string) (RuntimeProjection, error) {
	if db == nil {
		return RuntimeProjection{}, errors.New("database is not initialized")
	}
	if len(data) == 0 {
		var setting model.Setting
		result := db.Model(&model.Setting{}).Where("key = ?", "config").Limit(1).Find(&setting)
		if result.Error != nil {
			return RuntimeProjection{}, result.Error
		}
		if result.RowsAffected == 0 {
			data = defaultSingBoxBaseConfig
		} else {
			data = setting.Value
		}
	}
	eligibility, err := runtimeprojection.ValidateReferences(db, []byte(data))
	if err != nil {
		return RuntimeProjection{Eligibility: eligibility}, err
	}
	inbounds, err := b.InboundService.GetAllConfig(db)
	if err != nil {
		return RuntimeProjection{}, err
	}

	outbounds, err := b.OutboundService.GetAllConfig(db)
	if err != nil {
		return RuntimeProjection{}, err
	}

	services, err := b.ServicesService.GetAllConfig(db)
	if err != nil {
		return RuntimeProjection{}, err
	}

	endpoints, err := b.EndpointService.GetAllConfig(db)
	if err != nil {
		return RuntimeProjection{}, err
	}

	config, err := singboxconfig.BuildRuntimeConfig(json.RawMessage(data), singboxconfig.RuntimeSections{
		Inbounds:  inbounds,
		Outbounds: outbounds,
		Services:  services,
		Endpoints: endpoints,
	})
	return RuntimeProjection{Config: config, Eligibility: eligibility}, err
}
