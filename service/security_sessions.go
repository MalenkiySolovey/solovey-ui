package service

import (
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

const (
	DefaultSessionPageSize  = 50
	MaxSessionPageSize      = 200
	MaxSessionCleanupBatch  = 100
	sessionHistoryRetention = 30 * 24 * time.Hour
)

type SessionInventoryItem struct {
	Ref                 string `json:"ref"`
	Current             bool   `json:"current"`
	AuthState           string `json:"authState"`
	Assurance           string `json:"assurance"`
	LastMFAAt           int64  `json:"lastMfaAt"`
	LifetimePosture     string `json:"lifetimePosture"`
	CreatedAt           int64  `json:"createdAt"`
	AuthenticatedAt     int64  `json:"authenticatedAt"`
	LastSeenAt          int64  `json:"lastSeenAt"`
	IdleExpiresAt       int64  `json:"idleExpiresAt"`
	AbsoluteExpiresAt   int64  `json:"absoluteExpiresAt"`
	RememberedExpiresAt int64  `json:"rememberedExpiresAt"`
	ClientProvenance    string `json:"clientProvenance"`
	ClientPrefix        string `json:"clientPrefix"`
	DeviceLabel         string `json:"deviceLabel"`
	RevokedAt           int64  `json:"revokedAt"`
	RevokedReason       string `json:"revokedReason,omitempty"`
	ReplacementRef      string `json:"replacementRef,omitempty"`
}

type SessionInventory struct {
	Items      []SessionInventoryItem `json:"items"`
	NextCursor string                 `json:"nextCursor,omitempty"`
}

type SecuritySessionService struct {
	Now func() time.Time
}

func (s *SecuritySessionService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SecuritySessionService) List(userID uint, currentRef string, cursor string, limit int) (SessionInventory, error) {
	if _, err := s.CleanupExpired(MaxSessionCleanupBatch); err != nil {
		return SessionInventory{}, err
	}
	if limit <= 0 {
		limit = DefaultSessionPageSize
	}
	if limit > MaxSessionPageSize {
		limit = MaxSessionPageSize
	}
	query := dbsqlite.DB().Model(&model.SecuritySession{}).
		Where("user_id = ?", userID).
		Order("last_seen_at DESC, ref ASC").
		Limit(limit + 1)
	if strings.TrimSpace(cursor) != "" {
		var anchor model.SecuritySession
		if err := dbsqlite.DB().Model(&model.SecuritySession{}).
			Where("user_id = ? AND ref = ?", userID, cursor).
			First(&anchor).Error; err != nil {
			return SessionInventory{}, common.NewError("invalid session cursor")
		}
		query = query.Where("(last_seen_at < ?) OR (last_seen_at = ? AND ref > ?)", anchor.LastSeenAt, anchor.LastSeenAt, anchor.Ref)
	}
	var sessions []model.SecuritySession
	if err := query.Find(&sessions).Error; err != nil {
		return SessionInventory{}, err
	}
	result := SessionInventory{Items: make([]SessionInventoryItem, 0, min(limit, len(sessions)))}
	if len(sessions) > limit {
		result.NextCursor = sessions[limit-1].Ref
		sessions = sessions[:limit]
	}
	for _, row := range sessions {
		result.Items = append(result.Items, SessionInventoryItem{
			Ref:                 row.Ref,
			Current:             row.Ref == currentRef,
			AuthState:           row.AuthState,
			Assurance:           row.Assurance,
			LastMFAAt:           row.LastMFAAt,
			LifetimePosture:     row.LifetimePosture,
			CreatedAt:           row.CreatedAt,
			AuthenticatedAt:     row.AuthenticatedAt,
			LastSeenAt:          row.LastSeenAt,
			IdleExpiresAt:       row.IdleExpiresAt,
			AbsoluteExpiresAt:   row.AbsoluteExpiresAt,
			RememberedExpiresAt: row.RememberedExpiresAt,
			ClientProvenance:    row.ClientProvenance,
			ClientPrefix:        row.ClientPrefix,
			DeviceLabel:         row.DeviceLabel,
			RevokedAt:           row.RevokedAt,
			RevokedReason:       row.RevokedReason,
			ReplacementRef:      row.ReplacementRef,
		})
	}
	return result, nil
}

// CleanupExpired removes a bounded batch of stale metadata together with its
// backing session and any unconsumed grants. Recent revoked rows remain
// available to the administrator's inventory for forensic context.
func (s *SecuritySessionService) CleanupExpired(limit int) (int64, error) {
	if limit <= 0 || limit > MaxSessionCleanupBatch {
		limit = MaxSessionCleanupBatch
	}
	cutoff := s.now().Add(-sessionHistoryRetention).Unix()
	var rows []model.SecuritySession
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SecuritySession{}).
			Where("(revoked_at > 0 AND revoked_at <= ?) OR (absolute_expires_at > 0 AND absolute_expires_at <= ?)", cutoff, cutoff).
			Order("last_seen_at ASC").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		sessionIDs := make([]string, 0, len(rows))
		refs := make([]string, 0, len(rows))
		for _, row := range rows {
			sessionIDs = append(sessionIDs, row.SessionID)
			refs = append(refs, row.Ref)
		}
		if err := tx.Where("session_ref IN ?", refs).Delete(&model.StepUpGrant{}).Error; err != nil {
			return err
		}
		if tx.Migrator().HasTable("sessions") {
			if err := tx.Exec("DELETE FROM sessions WHERE id IN ?", sessionIDs).Error; err != nil {
				return err
			}
		}
		return tx.Where("session_id IN ?", sessionIDs).Delete(&model.SecuritySession{}).Error
	})
	return int64(len(rows)), err
}

func (s *SecuritySessionService) Revoke(userID uint, ref, reason string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return common.NewError("session reference is required")
	}
	if reason == "" {
		reason = "user_revoked"
	}
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		var row model.SecuritySession
		if err := tx.Model(&model.SecuritySession{}).
			Where("user_id = ? AND ref = ?", userID, ref).
			First(&row).Error; err != nil {
			return common.NewError("session not found")
		}
		if row.RevokedAt == 0 {
			if err := tx.Model(&model.SecuritySession{}).Where("session_id = ?", row.SessionID).
				Updates(map[string]any{
					"state":          SessionStateRevoked,
					"revoked_at":     s.now().Unix(),
					"revoked_reason": reason,
				}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("session_ref = ?", row.Ref).Delete(&model.StepUpGrant{}).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM sessions WHERE id = ?", row.SessionID).Error
	})
	if err != nil {
		return err
	}
	realtime.CloseSession(ref, "session_revoked")
	return nil
}

func (s *SecuritySessionService) RevokeOthers(userID uint, currentRef, reason string) (int64, error) {
	if reason == "" {
		reason = "other_sessions_revoked"
	}
	var rows []model.SecuritySession
	if err := dbsqlite.DB().Model(&model.SecuritySession{}).
		Where("user_id = ? AND ref <> ? AND revoked_at = 0", userID, currentRef).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, 0, len(rows))
		refs := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.SessionID)
			refs = append(refs, row.Ref)
		}
		if err := tx.Model(&model.SecuritySession{}).
			Where("user_id = ? AND ref <> ? AND session_id IN ? AND revoked_at = 0", userID, currentRef, ids).
			Updates(map[string]any{
				"state":          SessionStateRevoked,
				"revoked_at":     s.now().Unix(),
				"revoked_reason": reason,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("session_ref IN ?", refs).Delete(&model.StepUpGrant{}).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM sessions WHERE id IN ?", ids).Error
	})
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		realtime.CloseSession(row.Ref, "session_revoked")
	}
	return int64(len(rows)), nil
}

func (s *SecuritySessionService) Validate(ref string, userID uint, credentialGeneration, mfaGeneration uint64) (model.SecuritySession, error) {
	var row model.SecuritySession
	err := dbsqlite.DB().Model(&model.SecuritySession{}).
		Where("ref = ? AND user_id = ? AND credential_generation = ? AND mfa_generation = ? AND revoked_at = 0",
			ref, userID, credentialGeneration, mfaGeneration).
		First(&row).Error
	if err != nil {
		return row, common.NewError("invalid session")
	}
	now := s.now().Unix()
	if row.State == SessionStateRevoked ||
		(row.IdleExpiresAt > 0 && row.IdleExpiresAt <= now) ||
		(row.AbsoluteExpiresAt > 0 && row.AbsoluteExpiresAt <= now) ||
		(row.RememberedExpiresAt > 0 && row.RememberedExpiresAt <= now) {
		return row, common.NewError("expired session")
	}
	return row, nil
}
