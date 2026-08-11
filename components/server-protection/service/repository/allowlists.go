package repository

import (
	"context"
	"errors"
)

func (r *Repository) ListPortAllowlist(ctx context.Context, page PageQuery, protocol string) ([]PortAllowlistModel, int64, error) {
	query, err := r.query(ctx, &PortAllowlistModel{})
	if err != nil {
		return nil, 0, err
	}
	if protocol != "" {
		query = query.Where("protocol = ?", protocol)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []PortAllowlistModel
	err = query.Order("created_at DESC, id DESC").Offset(page.Offset()).Limit(page.Limit).Find(&items).Error
	return items, total, err
}

func (r *Repository) CreatePortAllowlist(ctx context.Context, item *PortAllowlistModel) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *Repository) DeletePortAllowlist(ctx context.Context, id uint) error {
	return r.deleteByID(ctx, &PortAllowlistModel{}, id)
}

func (r *Repository) ListIPAllowlist(ctx context.Context, page PageQuery) ([]IPAllowlistModel, int64, error) {
	query, err := r.query(ctx, &IPAllowlistModel{})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []IPAllowlistModel
	err = query.Order("created_at DESC, id DESC").Offset(page.Offset()).Limit(page.Limit).Find(&items).Error
	return items, total, err
}

func (r *Repository) CreateIPAllowlist(ctx context.Context, item *IPAllowlistModel) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *Repository) DeleteIPAllowlist(ctx context.Context, id uint) error {
	return r.deleteByID(ctx, &IPAllowlistModel{}, id)
}
