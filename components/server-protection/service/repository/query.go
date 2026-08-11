package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var ErrRecordNotFound = errors.New("server-protection record not found")

type PageQuery struct {
	Page  int
	Limit int
}

func (q PageQuery) Offset() int {
	page := q.Page
	if page < 1 {
		page = 1
	}
	if q.Limit <= 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/q.Limit {
		return maxInt
	}
	return (page - 1) * q.Limit
}

func (r *Repository) query(ctx context.Context, model any) (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Model(model), nil
}

func (r *Repository) deleteByID(ctx context.Context, model any, id uint) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	result := r.db.WithContext(ctx).Delete(model, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRecordNotFound
	}
	return nil
}
