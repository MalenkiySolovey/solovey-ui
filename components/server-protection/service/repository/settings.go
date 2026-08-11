package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

var ErrRevisionConflict = errors.New("server-protection revision conflict")

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) LoadSettings(ctx context.Context) (domain.Settings, bool, error) {
	value, _, degraded, err := r.LoadSettingsRevision(ctx)
	return value, degraded, err
}

func (r *Repository) LoadSettingsRevision(ctx context.Context) (domain.Settings, int, bool, error) {
	if r == nil || r.db == nil {
		return domain.Settings{}, 0, false, errors.New("server-protection repository is not initialized")
	}
	var model SettingsModel
	err := r.db.WithContext(ctx).Where("id = ?", 1).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := seedSettings(r.db.WithContext(ctx)); err != nil {
			return domain.Settings{}, 0, false, err
		}
		err = r.db.WithContext(ctx).Where("id = ?", 1).First(&model).Error
	}
	if err != nil {
		return domain.Settings{}, 0, false, err
	}
	value, err := settingsDomain(model)
	if err != nil || value.Validate() != nil {
		return domain.DefaultSettings(), model.Revision, true, nil
	}
	return value, model.Revision, false, nil
}

func (r *Repository) SaveSettings(ctx context.Context, value domain.Settings) error {
	_, revision, _, err := r.LoadSettingsRevision(ctx)
	if err != nil {
		return err
	}
	_, err = r.SaveSettingsRevision(ctx, value, revision)
	return err
}

func (r *Repository) SaveSettingsRevision(ctx context.Context, value domain.Settings, expectedRevision int) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("server-protection repository is not initialized")
	}
	if err := value.Validate(); err != nil {
		return 0, err
	}
	flags, err := json.Marshal(value.FeatureFlags)
	if err != nil {
		return 0, err
	}
	newRevision := 0
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current SettingsModel
		if err := tx.Where("id = ?", 1).First(&current).Error; err != nil {
			return err
		}
		if expectedRevision < 1 || current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		model := settingsModel(value, flags)
		model.ID = 1
		model.Revision = current.Revision + 1
		if err := tx.Save(&model).Error; err != nil {
			return err
		}
		newRevision = model.Revision
		return nil
	})
	return newRevision, err
}

func settingsDomain(model SettingsModel) (domain.Settings, error) {
	flags := map[string]bool{}
	if len(model.FeatureFlagsJSON) > 0 {
		if err := json.Unmarshal(model.FeatureFlagsJSON, &flags); err != nil {
			return domain.Settings{}, err
		}
	}
	return domain.Settings{
		Enabled:                    model.Enabled,
		RetentionGlobalLimit:       model.RetentionGlobalLimit,
		RetentionPerResourceLimit:  model.RetentionPerResourceLimit,
		DefaultScoreThreshold:      model.DefaultScoreThreshold,
		DefaultGraylistTTLSeconds:  model.DefaultGraylistTTLSeconds,
		DiagnosticsCacheTTLSeconds: model.DiagnosticsCacheTTLSeconds,
		ObservationBufferSize:      model.ObservationBufferSize,
		ObservationFlushIntervalMS: model.ObservationFlushIntervalMS,
		IPv6GraylistPrefixBits:     model.IPv6GraylistPrefixBits,
		MaxScore:                   model.MaxScore,
		SafeMetaMaxBytes:           model.SafeMetaMaxBytes,
		ClockSkewToleranceSeconds:  model.ClockSkewToleranceSeconds,
		ArtifactRetentionCount:     model.ArtifactRetentionCount,
		ArtifactRetentionDays:      model.ArtifactRetentionDays,
		AdvancedAcknowledgedAt:     model.AdvancedAcknowledgedAt,
		FeatureFlags:               flags,
	}, nil
}
