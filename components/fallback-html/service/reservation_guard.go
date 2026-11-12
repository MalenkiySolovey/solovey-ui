//go:build !minimal

package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	"gorm.io/gorm"
)

const reservationProviderID = authority.ProviderID

func guardSiteTargetMutation(tx *gorm.DB, siteID uint) error {
	return authority.GuardSiteMutation(tx, reservationProviderID, "site:"+strconv.FormatUint(uint64(siteID), 10), time.Now().UTC())
}

func (s *Service) guardedRuntimeMutation(siteID uint, mutate func(*gorm.DB) error) error {
	ctx := s.db.Statement.Context
	return authority.WithMutationLockContext(ctx, func() error {
		activated := false
		err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := guardSiteTargetMutation(tx, siteID); err != nil {
				return err
			}
			if err := mutate(tx); err != nil {
				return err
			}
			activated = true
			return s.runtime.RebuildContext(ctx, tx)
		})
		if err == nil || !activated || errors.Is(err, ErrRuntimeDraining) {
			return err
		}
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeDrainTimeout)
		defer cancel()
		if restoreErr := s.runtime.RebuildContext(recoveryCtx, s.db); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore fallback-html runtime after rollback: %w", restoreErr))
		}
		return err
	})
}

func (s *Service) guardedSiteMutation(siteID uint, mutate func(*gorm.DB) error, after func() error) error {
	return authority.WithMutationLock(func() error {
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := guardSiteTargetMutation(tx, siteID); err != nil {
				return err
			}
			return mutate(tx)
		}); err != nil {
			return err
		}
		if after != nil {
			return after()
		}
		return nil
	})
}
