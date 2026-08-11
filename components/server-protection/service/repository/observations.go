package repository

import (
	"context"
)

type EventFilter struct {
	PageQuery
	ResourceID string
	Kind       string
	Since      int64
}

func (r *Repository) ListEvents(ctx context.Context, filter EventFilter) ([]ProbeEventModel, int64, error) {
	query, err := r.query(ctx, &ProbeEventModel{})
	if err != nil {
		return nil, 0, err
	}
	if filter.ResourceID != "" {
		query = query.Where("resource_id = ?", filter.ResourceID)
	}
	if filter.Kind != "" {
		query = query.Where("signal_kind = ?", filter.Kind)
	}
	if filter.Since > 0 {
		query = query.Where("observed_at >= ?", filter.Since)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []ProbeEventModel
	err = query.Order("observed_at DESC, id DESC").Offset(filter.Offset()).Limit(filter.Limit).Find(&items).Error
	return items, total, err
}

func (r *Repository) ClearEvents(ctx context.Context, resourceID string) (int64, error) {
	query, err := r.query(ctx, &ProbeEventModel{})
	if err != nil {
		return 0, err
	}
	if resourceID != "" {
		query = query.Where("resource_id = ?", resourceID)
	} else {
		query = query.Where("1 = 1")
	}
	result := query.Delete(&ProbeEventModel{})
	return result.RowsAffected, result.Error
}

type GraylistFilter struct {
	PageQuery
	ResourceID string
	Family     int
}

func (r *Repository) ListGraylist(ctx context.Context, filter GraylistFilter) ([]GraylistModel, int64, error) {
	query, err := r.query(ctx, &GraylistModel{})
	if err != nil {
		return nil, 0, err
	}
	if filter.ResourceID != "" {
		query = query.Where("resource_id = ?", filter.ResourceID)
	}
	if filter.Family == 4 || filter.Family == 6 {
		query = query.Where("ip_family = ?", filter.Family)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []GraylistModel
	err = query.Order("updated_at DESC, id DESC").Offset(filter.Offset()).Limit(filter.Limit).Find(&items).Error
	return items, total, err
}

func (r *Repository) ClearGraylist(ctx context.Context, resourceID string) (int64, error) {
	query, err := r.query(ctx, &GraylistModel{})
	if err != nil {
		return 0, err
	}
	if resourceID != "" {
		query = query.Where("resource_id = ?", resourceID)
	} else {
		query = query.Where("1 = 1")
	}
	result := query.Delete(&GraylistModel{})
	return result.RowsAffected, result.Error
}
