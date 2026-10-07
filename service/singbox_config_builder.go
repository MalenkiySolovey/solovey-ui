package service

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	runtimeprojection "github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/logger"
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
	Config            []byte
	Eligibility       runtimeprojection.Report
	RuleCompatibility []singboxvalidation.RuleFinding
	DNSCompatibility  []diagnostics.Finding
}

func (b SingBoxConfigBuilder) BuildFromDB(db *gorm.DB, data string) ([]byte, error) {
	projection, err := b.BuildProjectionFromDB(db, data)
	return projection.Config, err
}

// BuildProjectionFromDB provides explicit omission diagnostics and validates
// all references against the candidate view before rule-set preparation.
func (b SingBoxConfigBuilder) BuildProjectionFromDB(db *gorm.DB, data string) (RuntimeProjection, error) {
	projection, err := b.BuildCandidateProjectionFromDB(db, data, len(data) == 0)
	if err != nil {
		return projection, err
	}
	projection.Config, err = singboxconfig.PrepareRuntimeAssets(projection.Config)
	return projection, err
}

// BuildCandidateProjectionFromDB renders and analyzes the candidate without
// preparing files, downloading assets, starting transports, or writing storage.
func (b SingBoxConfigBuilder) BuildCandidateProjectionFromDB(db *gorm.DB, data string, upgrade bool) (RuntimeProjection, error) {
	if db == nil {
		return RuntimeProjection{}, errors.New("database is not initialized")
	}
	stored := len(data) == 0
	if stored {
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
	var compatibility []singboxvalidation.RuleFinding
	if upgrade {
		upgrade, err := singboxvalidation.PrepareRuleUpgrade([]byte(data))
		compatibility = upgrade.Findings
		if err != nil {
			return RuntimeProjection{RuleCompatibility: compatibility}, err
		}
		data = string(upgrade.Candidate)
		for _, finding := range compatibility {
			logger.Warningf("config rule compatibility: %s [%s]: %s", finding.Path, finding.Code, finding.Message)
		}
	} else if _, err := singboxvalidation.ValidateRuleConditions([]byte(data)); err != nil {
		return RuntimeProjection{}, err
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

	config, err := singboxconfig.MergeRuntimeConfig(json.RawMessage(data), singboxconfig.RuntimeSections{
		Inbounds:  inbounds,
		Outbounds: outbounds,
		Services:  services,
		Endpoints: endpoints,
	})
	projection := RuntimeProjection{Config: config, Eligibility: eligibility, RuleCompatibility: compatibility}
	if err != nil {
		return projection, err
	}
	if upgrade {
		upgraded, upgradeErr := singboxconfig.PrepareDNSUpgrade(config)
		projection.DNSCompatibility = upgraded.Findings
		projection.Config = upgraded.Candidate
		err = upgradeErr
	} else {
		projection.DNSCompatibility, err = singboxconfig.ValidateDNSConfig(config)
	}
	projection.DNSCompatibility = append(projection.DNSCompatibility, entityinbounds.TUNDNSFindings(config)...)
	if err == nil {
		err = diagnostics.FirstError(projection.DNSCompatibility)
	}
	if err != nil {
		projection.Config = config
	}
	return projection, err
}
