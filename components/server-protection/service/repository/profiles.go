package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type ProfileFilter struct {
	PageQuery
	ResourceID string
	Status     string
}

func (r *Repository) ListProfiles(ctx context.Context, filter ProfileFilter) ([]ProfileModel, int64, error) {
	query, err := r.query(ctx, &ProfileModel{})
	if err != nil {
		return nil, 0, err
	}
	if filter.ResourceID != "" {
		query = query.Where("resource_id = ?", filter.ResourceID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []ProfileModel
	err = query.Order("updated_at DESC, id DESC").Offset(filter.Offset()).Limit(filter.Limit).Find(&items).Error
	return items, total, err
}

func (r *Repository) Profile(ctx context.Context, id uint) (ProfileModel, error) {
	if r == nil || r.db == nil {
		return ProfileModel{}, errors.New("server-protection repository is not initialized")
	}
	var item ProfileModel
	err := r.db.WithContext(ctx).First(&item, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProfileModel{}, ErrRecordNotFound
	}
	return item, err
}

func (r *Repository) CreateProfile(ctx context.Context, item *ProfileModel) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *Repository) UpdateProfile(ctx context.Context, item *ProfileModel, expectedRevision int) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current ProfileModel
		if err := tx.First(&current, item.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRecordNotFound
			}
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		item.Revision = expectedRevision + 1
		return tx.Save(item).Error
	})
}

func (r *Repository) DeleteProfile(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	result := r.db.WithContext(ctx).Delete(&ProfileModel{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRecordNotFound
	}
	return nil
}
