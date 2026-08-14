//go:build !minimal

package service

import (
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
	return authority.WithMutationLock(func() error {
		err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := guardSiteTargetMutation(tx, siteID); err != nil {
				return err
			}
			if err := mutate(tx); err != nil {
				return err
			}
			return s.runtime.Rebuild(tx)
		})
		if err == nil {
			return nil
		}
		if restoreErr := s.runtime.Rebuild(s.db); restoreErr != nil {
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
