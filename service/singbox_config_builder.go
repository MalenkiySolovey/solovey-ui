package service

import (
	"encoding/json"
	"errors"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/assembly"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
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
type RuntimeProjection = assembly.RuntimeProjection

// PrepareHTTPDownloadsFromDB adapts a new/edit/import candidate to the shared
// HTTP owner using the transaction's actual outbound catalogue. It publishes
// nothing and validates all other current semantics before returning base data.
func (b SingBoxConfigBuilder) PrepareHTTPDownloadsFromDB(db *gorm.DB, data string) (string, RuntimeProjection, error) {
	projection, err := b.BuildCandidateProjectionFromDB(db, data, false)
	if len(projection.Config) == 0 {
		return data, projection, err
	}
	if err != nil && diagnostics.FirstError(projection.HTTPCompatibility) == nil {
		return data, projection, err
	}
	prepared, err := singboxconfig.PrepareHTTPDownloads(projection.Config)
	projection.HTTPCompatibility = prepared.Findings
	if err != nil {
		return data, projection, err
	}
	var stored, complete map[string]json.RawMessage
	if json.Unmarshal([]byte(data), &stored) != nil || json.Unmarshal(prepared.Candidate, &complete) != nil {
		return data, projection, errors.New("HTTP candidate must be an object")
	}
	for _, key := range []string{"http_clients", "route"} {
		if raw, present := complete[key]; present {
			stored[key] = raw
		}
	}
	base, err := json.Marshal(stored)
	if err != nil {
		return data, projection, err
	}
	validated, err := b.BuildCandidateProjectionFromDB(db, string(base), false)
	validated.HTTPCompatibility = append(prepared.Findings, validated.HTTPCompatibility...)
	if err != nil {
		validated.Config = projection.Config
		return data, validated, err
	}
	if err := singboxvalidation.ValidateConfigShape(validated.Config); err != nil {
		validated.Config = projection.Config
		return data, validated, err
	}
	return string(base), validated, nil
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
	return assembly.BuildCandidateProjectionFromDB(db, data, upgrade)
}
