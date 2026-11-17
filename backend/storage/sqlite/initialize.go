package sqlite

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
)

// Bootstrap supplies the existing schema owner before and after the connection
// opens. Both callbacks run inside the single initialization lifetime. They do
// not acquire a second connection owner or commit a separate transaction.
type Bootstrap struct {
	Debug      bool
	BeforeOpen func() error
	AfterOpen  func(*gorm.DB) error
}

// InitializeContext serializes bootstrap with the existing connection and
// maintenance generations. Schema migration remains with the supplied owner.
func InitializeContext(ctx context.Context, dbPath string, bootstrap Bootstrap) (err error) {
	if ctx == nil {
		return errors.New("sqlite initialization context is required")
	}
	if bootstrap.BeforeOpen == nil || bootstrap.AfterOpen == nil {
		return errors.New("sqlite bootstrap owner is required")
	}
	initMu.Lock()
	defer initMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepareForInit(ctx); err != nil {
		return err
	}
	if err := bootstrap.BeforeOpen(); err != nil {
		return err
	}
	if err := openContext(ctx, dbPath, bootstrap.Debug); err != nil {
		return err
	}
	initialized := false
	defer func() {
		if !initialized {
			if closeErr := CloseContext(ctx); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close failed database initialization: %w", closeErr))
			}
		}
	}()
	if err := bootstrap.AfterOpen(DB().WithContext(ctx)); err != nil {
		return err
	}
	initialized = true
	return nil
}
