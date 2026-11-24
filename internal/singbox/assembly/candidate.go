// Package assembly owns complete candidate assembly for runtime, save and migration.
package assembly

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbschema "github.com/MalenkiySolovey/solovey-ui/database/schema"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
	runtimeprojection "github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	entityservices "github.com/MalenkiySolovey/solovey-ui/internal/entities/services"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

type RuntimeProjection struct {
	Config                 []byte
	Eligibility            runtimeprojection.Report
	RuleCompatibility      []singboxvalidation.RuleFinding
	DNSCompatibility       []diagnostics.Finding
	HTTPCompatibility      []diagnostics.Finding
	TLSCompatibility       []diagnostics.Finding
	TransportCompatibility []diagnostics.Finding
	OptionsCompatibility   []diagnostics.Finding
}

func BuildCandidateProjectionFromDB(db *gorm.DB, data string, upgrade bool) (RuntimeProjection, error) {
	return buildCandidateProjectionFromDB(db, data, upgrade, entityoutbounds.GetAllConfig)
}

func BuildCoreStartupFromDB(db *gorm.DB, admit entityoutbounds.ZapretBatchAdmitter) ([]byte, error) {
	bound, err := entityoutbounds.CheckZapretStartup(db, admit != nil)
	if err != nil {
		return nil, err
	}
	render := entityoutbounds.GetAllConfig
	if bound {
		tun, err := InstalledZapretTUNRequest(db)
		if err != nil {
			return nil, err
		}
		render = func(tx *gorm.DB) ([]json.RawMessage, error) {
			return entityoutbounds.GetAllRuntimeConfig(tx, func(input zapretactivation.Validation) ([]zapretactivation.Grant, error) {
				input.TUN = tun
				return admit(input)
			})
		}
	}
	projection, err := buildCandidateProjectionFromDB(db, "", true, render)
	if err != nil {
		return nil, err
	}
	if err := validateZapretCoreProfile(projection.Config, bound); err != nil {
		return nil, err
	}
	return singboxconfig.PrepareRuntimeAssets(projection.Config)
}

// ValidateCoreRestartFromDB checks the same desired candidate without starting
// a Core, acquiring a Zapret generation or returning a runnable config.
func ValidateCoreRestartFromDB(db *gorm.DB, zapretReady bool) error {
	bound, err := entityoutbounds.CheckZapretStartup(db, zapretReady)
	if err != nil {
		return err
	}
	render := entityoutbounds.GetAllConfig
	if bound {
		render = entityoutbounds.ProjectZapretForValidation
	}
	projection, err := buildCandidateProjectionFromDB(db, "", true, render)
	if err != nil {
		return err
	}
	if err := validateZapretCoreProfile(projection.Config, bound); err != nil {
		return err
	}
	var selected map[string]bool
	if bound {
		requests, err := entityoutbounds.ZapretRequests(db)
		if err != nil {
			return err
		}
		selected = make(map[string]bool, len(requests))
		for _, request := range requests {
			selected[request.OutboundRef] = true
		}
		if singboxvalidation.ValidateConfigShape(projection.Config) != nil {
			return fmt.Errorf("%w: the pinned Core rejected the candidate", entityoutbounds.ErrZapretPacketPathUnqualified)
		}
	}
	if err := singboxconfig.ValidateDialedDNSDetours(projection.Config, selected); err != nil {
		return err
	}
	_, err = singboxconfig.PrepareRuntimeAssets(projection.Config)
	return err
}

func validateZapretCoreProfile(config []byte, bound bool) error {
	var candidate struct {
		Inbounds  []json.RawMessage
		Route     map[string]json.RawMessage
		Outbounds []map[string]json.RawMessage
		Endpoints []map[string]json.RawMessage
	}
	if err := json.Unmarshal(config, &candidate); err != nil {
		return err
	}
	if _, err := installedZapretTUNRequest(candidate.Inbounds, bound); err != nil {
		return err
	}
	if !bound {
		return nil
	}
	for key := range candidate.Route {
		switch strings.ToLower(key) {
		case "default_mark", "default_network_namespace", "network_namespaces", "default_interface", "auto_detect_interface":
			return fmt.Errorf("%w: conflicting Core route control %s", entityoutbounds.ErrZapretPacketPathUnqualified, key)
		}
	}
	for _, options := range append(candidate.Outbounds, candidate.Endpoints...) {
		for key := range options {
			if strings.EqualFold(key, "routing_mark") {
				return fmt.Errorf("%w: another Core socket mark owner is configured", entityoutbounds.ErrZapretPacketPathUnqualified)
			}
		}
	}
	return nil
}

// ValidateHeldRestoreCandidate checks the complete offline provider config
// without exporting a configuration that could be passed to Core.Start.
func ValidateHeldRestoreCandidate(db *gorm.DB, projectFiles func([]byte) ([]byte, error)) error {
	projection, err := buildCandidateProjectionFromDB(db, "", false, entityoutbounds.ProjectHeldRestoreForValidation)
	if err != nil {
		return err
	}
	if err := singboxvalidation.ValidateConfigShape(projection.Config); err != nil {
		return err
	}
	config, err := projectFiles(projection.Config)
	if err != nil {
		return err
	}
	return singboxvalidation.ValidateConfig(config)
}

func buildCandidateProjectionFromDB(db *gorm.DB, data string, upgrade bool, renderOutbounds func(*gorm.DB) ([]json.RawMessage, error)) (RuntimeProjection, error) {
	if db == nil {
		return RuntimeProjection{}, errors.New("database is not initialized")
	}
	stored := len(data) == 0
	if upgrade && stored {
		legacy, err := dbschema.LegacyProjection(db)
		if err != nil {
			return RuntimeProjection{}, err
		}
		upgrade = legacy
	}
	if stored {
		var setting model.Setting
		result := db.Model(&model.Setting{}).Where("key = ?", "config").Limit(1).Find(&setting)
		if result.Error != nil {
			return RuntimeProjection{}, result.Error
		}
		if result.RowsAffected == 0 {
			data = singboxconfig.DefaultBaseConfig
		} else {
			data = setting.Value
		}
	}
	sourceBase := data
	var optionCompatibility []diagnostics.Finding
	var compatibility []singboxvalidation.RuleFinding
	if upgrade {
		base, err := singboxconfig.PrepareBaseOptionsUpgrade([]byte(data))
		optionCompatibility = base.Findings
		if err != nil {
			return RuntimeProjection{OptionsCompatibility: optionCompatibility}, err
		}
		data = string(base.Candidate)
		upgrade, err := singboxvalidation.PrepareRuleUpgrade([]byte(data))
		compatibility = upgrade.Findings
		if err != nil {
			return RuntimeProjection{RuleCompatibility: compatibility, OptionsCompatibility: optionCompatibility}, err
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
	inbounds, err := optionalSection(db, &model.Inbound{}, func() ([]json.RawMessage, error) {
		return entityinbounds.GetAllConfig(db, entityinbounds.StoredUsers{})
	})
	if err != nil {
		return RuntimeProjection{}, err
	}

	outbounds, err := optionalSection(db, &model.Outbound{}, func() ([]json.RawMessage, error) { return renderOutbounds(db) })
	if err != nil {
		return RuntimeProjection{}, err
	}

	services, err := optionalSection(db, &model.Service{}, func() ([]json.RawMessage, error) { return entityservices.GetAllConfig(db) })
	if err != nil {
		return RuntimeProjection{}, err
	}

	endpoints, err := optionalSection(db, &model.Endpoint{}, func() ([]json.RawMessage, error) { return entityendpoints.GetAllConfig(db) })
	if err != nil {
		return RuntimeProjection{}, err
	}

	sections := singboxconfig.RuntimeSections{
		Inbounds:  inbounds,
		Outbounds: outbounds,
		Services:  services,
		Endpoints: endpoints,
	}
	config, err := singboxconfig.MergeRuntimeConfig(json.RawMessage(data), sections)
	projection := RuntimeProjection{Config: config, Eligibility: eligibility, RuleCompatibility: compatibility, OptionsCompatibility: optionCompatibility}
	if err != nil {
		return projection, err
	}
	original := config
	var dnsAvailability []diagnostics.Finding
	config, dnsAvailability, err = singboxconfig.ProjectDNSCapabilities(config)
	projection.DNSCompatibility = append(projection.DNSCompatibility, dnsAvailability...)
	if err != nil {
		return projection, err
	}
	projection.Config = config
	if sourceBase != data {
		original, err = singboxconfig.MergeRuntimeConfig(json.RawMessage(sourceBase), sections)
		if err != nil {
			return projection, err
		}
	}
	definitions, definitionErr := entitytls.ReadProviderDefinitions(db)
	if definitionErr != nil {
		return projection, definitionErr
	}
	certificates, certificateErr := entitytls.ProjectCertificateProviders(config, definitions)
	projection.TLSCompatibility = certificates.Findings
	if certificateErr != nil {
		projection.Config = original
		return projection, certificateErr
	}
	config = certificates.Candidate
	projection.Config = config
	projection.OptionsCompatibility = append(projection.OptionsCompatibility, singboxvalidation.OwnerOptionsFindings(config)...)
	if err := diagnostics.FirstError(projection.OptionsCompatibility); err != nil {
		projection.Config = original
		return projection, err
	}
	if upgrade {
		transports, transportErr := entityprotocol.PrepareConfigUpgrade(config)
		projection.TransportCompatibility = transports.Findings
		if transportErr != nil {
			projection.Config = original
			return projection, transportErr
		}
		config = transports.Candidate
		projection.Config = config
	}
	projection.TransportCompatibility = append(projection.TransportCompatibility, entityprotocol.ConfigFindings(config)...)
	projection.TLSCompatibility = append(projection.TLSCompatibility, entitytls.TLSConfigFindings(config)...)
	if semanticErr := diagnostics.FirstError(append(append([]diagnostics.Finding{}, projection.TLSCompatibility...), projection.TransportCompatibility...)); semanticErr != nil {
		projection.Config = original
		return projection, semanticErr
	}
	if upgrade {
		upgraded, upgradeErr := singboxconfig.PrepareHTTPUpgrade(config)
		projection.HTTPCompatibility = upgraded.Findings
		projection.Config = upgraded.Candidate
		err = upgradeErr
	} else {
		projection.HTTPCompatibility, err = singboxconfig.ValidateHTTPConfig(config)
	}
	if err != nil {
		projection.Config = original
		return projection, err
	}
	config = projection.Config
	if upgrade {
		upgraded, upgradeErr := singboxconfig.PrepareDNSUpgrade(config)
		projection.DNSCompatibility = append(projection.DNSCompatibility, upgraded.Findings...)
		projection.Config = upgraded.Candidate
		err = upgradeErr
	} else {
		var findings []diagnostics.Finding
		findings, err = singboxconfig.ValidateDNSConfig(config)
		projection.DNSCompatibility = append(projection.DNSCompatibility, findings...)
	}
	projection.DNSCompatibility = append(projection.DNSCompatibility, entityinbounds.TUNDNSFindings(config)...)
	projection.DNSCompatibility = append(projection.DNSCompatibility, singboxconfig.CurrentDNSRuntimeFindings(projection.Config)...)
	if err == nil {
		err = diagnostics.FirstError(projection.DNSCompatibility)
	}
	if err != nil {
		projection.Config = original
	}
	return projection, err
}

func optionalSection(db *gorm.DB, model any, render func() ([]json.RawMessage, error)) ([]json.RawMessage, error) {
	if !db.Migrator().HasTable(model) {
		return []json.RawMessage{}, nil
	}
	return render()
}
