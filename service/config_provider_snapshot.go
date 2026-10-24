package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"strings"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/targetcheck"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
	"gorm.io/gorm"
)

var ErrProviderConfigurationUnavailable = errors.New("provider configuration snapshot unavailable")

const maxProviderDesiredStateBytes = 64 << 20

var _ updateservice.CoreConfigurationOwner = (*ConfigService)(nil)

// CaptureProviderConfiguration uses one database view and the established core
// serializer. It performs no asset preparation, upgrades, writes or transport
// starts. Inactive rows remain bound to the revision even when the current
// runtime projection cannot include them. Raw credentials never enter its JSON.
func (s *ConfigService) CaptureProviderConfiguration(ctx context.Context) (result updateservice.CoreConfigurationSnapshot, err error) {
	if ctx == nil || s == nil || s.coreInstance() == nil || s.runtime().restart() == nil {
		return result, ErrProviderConfigurationUnavailable
	}
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return result, ErrProviderConfigurationUnavailable
	}
	defer release()
	err = s.runtime().restart().RunBlockingContext(ctx, func() error {
		db := configDatabase()
		if db == nil {
			return ErrProviderConfigurationUnavailable
		}
		status := s.coreInstance().RuntimeStatus(ctx)
		if status.State == "running" && (!status.APIAvailable || status.Generation == "") {
			return coreruntime.ErrRuntimeAPIUnavailable
		}
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			settings := SettingService{database: tx}
			held, err := settings.CoreMaintenance()
			if err != nil {
				return ErrProviderConfigurationUnavailable
			}
			projection, err := s.singBoxConfigBuilder().BuildCandidateProjectionFromDB(tx, "", false)
			if err != nil || len(projection.Config) == 0 || len(projection.Config) > targetcheck.MaxConfigBytes {
				clear(projection.Config)
				return ErrProviderConfigurationUnavailable
			}
			revision, err := providerDesiredRevision(tx)
			if err != nil {
				clear(projection.Config)
				return ErrProviderConfigurationUnavailable
			}
			capabilities := entitycapabilities.Build()
			if capabilities.Validate(capabilities.Provider, capabilities.ComponentProfile) != nil {
				clear(projection.Config)
				return ErrProviderConfigurationUnavailable
			}
			result = updateservice.CoreConfigurationSnapshot{
				Configuration: projection.Config, ConfigurationDigest: providerSnapshotDigest(projection.Config),
				ConfigurationRevision: revision, Maintenance: held,
				RuntimeGeneration: status.Generation, RuntimeState: strings.ToUpper(status.State),
				CapabilityRevision: capabilities.Revision(), Capabilities: capabilities, Preserved: []updateservice.PreservedConfiguration{},
			}
			if held {
				if status.State == "running" {
					result.Clear()
					return ErrProviderConfigurationUnavailable
				}
				result.RuntimeState = "MAINTENANCE"
			}
			for _, omitted := range projection.Eligibility.Omitted {
				result.Preserved = append(result.Preserved, updateservice.PreservedConfiguration{
					Category: omitted.Category, ID: omitted.ID, Type: omitted.Type, Reason: omitted.Capability.Reason,
				})
			}
			return ctx.Err()
		})
	})
	if err != nil {
		result.Clear()
		return updateservice.CoreConfigurationSnapshot{}, ErrProviderConfigurationUnavailable
	}
	return result, nil
}

func providerSnapshotDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Hash actual columns rather than editor DTOs: several model MarshalJSON
// methods intentionally hide credentials, Options or provider envelopes. The
// revision includes every stored entity, even an ineligible or disabled row.
// Traffic observations are excluded; enable/quota/expiry intent is included.
func providerDesiredRevision(db *gorm.DB) (string, error) {
	sum := sha256.New()
	_, _ = sum.Write([]byte("solovey.config-provider-checkpoint/v1\n"))
	remaining := int64(maxProviderDesiredStateBytes)
	for _, owner := range []any{&model.Inbound{}, &model.Outbound{}, &model.Endpoint{}, &model.Service{}, &model.Tls{}, &model.TLSCertificateProvider{}, &model.InboundDraft{}, &model.Client{}} {
		query := db.Model(owner).Order("id")
		if _, ok := owner.(*model.Client); ok {
			query = query.Omit("down", "up", "last_online", "last_ip_count", "total_up", "total_down")
		}
		if err := hashProviderDesiredRows(sum, query, &remaining); err != nil {
			return "", err
		}
	}
	if err := hashProviderDesiredRows(sum, db.Model(&model.Setting{}).Where("key = ?", "config").Order("id"), &remaining); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func hashProviderDesiredRows(sum hash.Hash, query *gorm.DB, remaining *int64) error {
	rows, err := query.Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	if err := hashProviderDesiredJSON(sum, columns, remaining); err != nil {
		return err
	}
	for rows.Next() {
		if err := query.Statement.Context.Err(); err != nil {
			return err
		}
		values := make([]any, len(columns))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		typed := make([]providerDesiredValue, len(values))
		for i, value := range values {
			typed[i].Value = value
			switch value.(type) {
			case nil:
				typed[i].Kind = "null"
			case string:
				typed[i].Kind = "text"
			case []byte:
				typed[i].Kind = "blob"
			case int64:
				typed[i].Kind = "integer"
			case float64:
				typed[i].Kind = "real"
			case bool:
				typed[i].Kind = "boolean"
			case time.Time:
				typed[i].Kind = "time"
			default:
				return ErrProviderConfigurationUnavailable
			}
		}
		if err := hashProviderDesiredJSON(sum, typed, remaining); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, _ = sum.Write([]byte("end\n"))
	return nil
}

type providerDesiredValue struct {
	Kind  string
	Value any
}

func hashProviderDesiredJSON(sum hash.Hash, value any, remaining *int64) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	defer clear(data)
	*remaining -= int64(len(data)) + 1
	if *remaining < 0 {
		return ErrProviderConfigurationUnavailable
	}
	_, _ = sum.Write(data)
	_, _ = sum.Write([]byte{'\n'})
	return nil
}
