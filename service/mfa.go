package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/util/password"
	totputil "github.com/MalenkiySolovey/solovey-ui/util/totp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MFAStatePending               = "pending"
	MFAStatePendingAcknowledgment = "pending_acknowledgment"
	MFAStateActive                = "active"
	MFAStateActivePendingRotation = "active_pending_rotation"
	MFAStateActiveRotationAck     = "active_rotation_ack"
	MFAStateActiveRecoveryAck     = "active_recovery_ack"

	MFAPendingTTL     = 10 * time.Minute
	RecoveryCodeCount = 10
	RecoveryCodeBytes = 16
)

var recoveryBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

type MFAStatus struct {
	Enabled                bool  `json:"enabled"`
	Pending                bool  `json:"pending"`
	PendingExpiresAt       int64 `json:"pendingExpiresAt"`
	RecoveryAcknowledged   bool  `json:"recoveryAcknowledged"`
	RecoveryRemaining      int64 `json:"recoveryRemaining"`
	AwaitingAcknowledgment bool  `json:"awaitingAcknowledgment"`
}

type MFAEnrollment struct {
	Secret    string `json:"secret"`
	URI       string `json:"uri"`
	ExpiresAt int64  `json:"expiresAt"`
}

type MFAService struct {
	SettingService SettingService
	Now            func() time.Time
}

func (s *MFAService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *MFAService) Status(userID uint) (MFAStatus, error) {
	var factor model.AdminMFAFactor
	err := dbsqlite.DB().Model(&model.AdminMFAFactor{}).Where("user_id = ?", userID).First(&factor).Error
	if dbsqlite.IsNotFound(err) {
		return MFAStatus{}, nil
	}
	if err != nil {
		return MFAStatus{}, err
	}
	var remaining int64
	if factor.RecoveryGeneration > 0 {
		if err := dbsqlite.DB().Model(&model.AdminRecoveryCode{}).
			Where("user_id = ? AND generation = ? AND used_at = 0", userID, factor.RecoveryGeneration).
			Count(&remaining).Error; err != nil {
			return MFAStatus{}, err
		}
	}
	now := s.now().Unix()
	return MFAStatus{
		Enabled:                isActiveMFAState(factor.State),
		Pending:                factor.PendingSecretCiphertext != "" && factor.PendingExpiresAt > now,
		PendingExpiresAt:       factor.PendingExpiresAt,
		RecoveryAcknowledged:   factor.RecoveryAcknowledged,
		RecoveryRemaining:      remaining,
		AwaitingAcknowledgment: isMFAAcknowledgmentState(factor.State) && factor.PendingExpiresAt > now,
	}, nil
}

func (s *MFAService) BeginEnrollment(userID uint, username string) (MFAEnrollment, error) {
	if userID == 0 || strings.TrimSpace(username) == "" {
		return MFAEnrollment{}, common.NewError("invalid administrator")
	}
	secret, err := totputil.GenerateSecret()
	if err != nil {
		return MFAEnrollment{}, err
	}
	encrypted, err := s.SettingService.encryptSettingValue(mfaSecretKey(userID), secret)
	if err != nil {
		return MFAEnrollment{}, err
	}
	now := s.now()
	expiresAt := now.Add(MFAPendingTTL).Unix()
	state := MFAStatePending
	var current model.AdminMFAFactor
	err = dbsqlite.DB().Model(&model.AdminMFAFactor{}).Where("user_id = ?", userID).First(&current).Error
	if err == nil {
		if current.ActiveSecretCiphertext != "" {
			state = MFAStateActivePendingRotation
		}
	} else if !dbsqlite.IsNotFound(err) {
		return MFAEnrollment{}, err
	}
	factor := model.AdminMFAFactor{
		UserID:                    userID,
		State:                     state,
		PendingSecretCiphertext:   encrypted,
		PendingExpiresAt:          expiresAt,
		PendingAcceptedCounter:    -1,
		PendingRecoveryGeneration: 0,
		UpdatedAt:                 now.Unix(),
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		if current.PendingRecoveryGeneration > 0 {
			if err := tx.Where("user_id = ? AND generation = ?", userID, current.PendingRecoveryGeneration).
				Delete(&model.AdminRecoveryCode{}).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"state", "pending_secret_ciphertext", "pending_expires_at",
				"pending_accepted_counter", "pending_recovery_generation", "updated_at",
			}),
		}).Create(&factor).Error
	}); err != nil {
		return MFAEnrollment{}, err
	}
	uri, err := totputil.ProvisioningURI(secret, username, "Solovey UI")
	if err != nil {
		return MFAEnrollment{}, err
	}
	return MFAEnrollment{Secret: secret, URI: uri, ExpiresAt: expiresAt}, nil
}

func (s *MFAService) ConfirmEnrollment(userID uint, code string) ([]string, error) {
	now := s.now()
	var factor model.AdminMFAFactor
	if err := dbsqlite.DB().Model(&model.AdminMFAFactor{}).Where("user_id = ?", userID).First(&factor).Error; err != nil {
		return nil, err
	}
	if factor.PendingSecretCiphertext == "" || factor.PendingExpiresAt <= now.Unix() {
		return nil, common.NewError("MFA enrollment expired")
	}
	if factor.State != MFAStatePending && factor.State != MFAStateActivePendingRotation {
		return nil, common.NewError("MFA enrollment is not awaiting confirmation")
	}
	secret, err := s.SettingService.decryptSettingValue(mfaSecretKey(userID), factor.PendingSecretCiphertext)
	if err != nil {
		return nil, err
	}
	counter, err := totputil.Verify(secret, code, now, -1)
	if err != nil {
		return nil, common.NewError("invalid MFA code")
	}
	nextGeneration := factor.RecoveryGeneration + 1
	if factor.PendingRecoveryGeneration >= nextGeneration {
		nextGeneration = factor.PendingRecoveryGeneration + 1
	}
	codes, rows, generation, err := s.generateRecoveryRows(userID, nextGeneration, now)
	if err != nil {
		return nil, err
	}
	nextState := MFAStatePendingAcknowledgment
	if factor.ActiveSecretCiphertext != "" {
		nextState = MFAStateActiveRotationAck
	}
	err = dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.AdminMFAFactor{}).
			Where("user_id = ? AND state = ? AND pending_secret_ciphertext = ? AND pending_expires_at > ?",
				userID, factor.State, factor.PendingSecretCiphertext, now.Unix()).
			Updates(map[string]any{
				"state":                       nextState,
				"pending_accepted_counter":    counter,
				"pending_recovery_generation": generation,
				"recovery_acknowledged":       false,
				"updated_at":                  now.Unix(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return common.NewError("MFA enrollment changed")
		}
		if err := tx.Where("user_id = ? AND generation = ?", userID, generation).
			Delete(&model.AdminRecoveryCode{}).Error; err != nil {
			return err
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *MFAService) AcknowledgeRecoveryCodes(userID uint) error {
	now := s.now().Unix()
	return dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		var factor model.AdminMFAFactor
		if err := tx.Model(&model.AdminMFAFactor{}).
			Where("user_id = ? AND state IN ? AND pending_recovery_generation > 0 AND pending_expires_at > ?",
				userID,
				[]string{MFAStatePendingAcknowledgment, MFAStateActiveRotationAck, MFAStateActiveRecoveryAck},
				now,
			).
			First(&factor).Error; err != nil {
			return common.NewError("recovery acknowledgement is not pending")
		}
		updates := map[string]any{
			"state":                       MFAStateActive,
			"recovery_generation":         factor.PendingRecoveryGeneration,
			"pending_recovery_generation": 0,
			"recovery_acknowledged":       true,
			"updated_at":                  now,
		}
		if factor.State != MFAStateActiveRecoveryAck {
			updates["active_secret_ciphertext"] = factor.PendingSecretCiphertext
			updates["pending_secret_ciphertext"] = ""
			updates["pending_expires_at"] = 0
			updates["last_accepted_counter"] = factor.PendingAcceptedCounter
			updates["pending_accepted_counter"] = -1
		} else {
			updates["pending_expires_at"] = 0
		}
		result := tx.Model(&model.AdminMFAFactor{}).
			Where("user_id = ? AND state = ? AND pending_recovery_generation = ?",
				userID, factor.State, factor.PendingRecoveryGeneration).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return common.NewError("MFA factor changed")
		}
		if err := tx.Where("user_id = ? AND generation <> ?", userID, factor.PendingRecoveryGeneration).
			Delete(&model.AdminRecoveryCode{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.User{}).Where("id = ?", userID).
			Update("mfa_generation", gorm.Expr("mfa_generation + 1")).Error
	})
}

func (s *MFAService) VerifyTOTP(userID uint, code string) error {
	now := s.now()
	var factor model.AdminMFAFactor
	if err := dbsqlite.DB().Model(&model.AdminMFAFactor{}).
		Where("user_id = ? AND state IN ?", userID, activeMFAStates()).
		First(&factor).Error; err != nil {
		return common.NewError("MFA is not enabled")
	}
	secret, err := s.SettingService.decryptSettingValue(mfaSecretKey(userID), factor.ActiveSecretCiphertext)
	if err != nil {
		return err
	}
	counter, err := totputil.Verify(secret, code, now, factor.LastAcceptedCounter)
	if err != nil {
		return common.NewError("invalid MFA code")
	}
	result := dbsqlite.DB().Model(&model.AdminMFAFactor{}).
		Where(
			"user_id = ? AND active_secret_ciphertext = ? AND state IN ? AND last_accepted_counter < ?",
			userID, factor.ActiveSecretCiphertext, activeMFAStates(), counter,
		).
		Updates(map[string]any{"last_accepted_counter": counter, "updated_at": now.Unix()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return common.NewError("MFA code already used")
	}
	return nil
}

func (s *MFAService) ConsumeRecoveryCode(userID uint, code string) error {
	verifier, err := s.recoveryVerifier(userID, code)
	if err != nil {
		return common.NewError("invalid recovery code")
	}
	now := s.now().Unix()
	return dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		var factor model.AdminMFAFactor
		if err := tx.Model(&model.AdminMFAFactor{}).
			Where("user_id = ? AND state IN ?", userID, activeMFAStates()).
			First(&factor).Error; err != nil {
			return common.NewError("MFA is not enabled")
		}
		result := tx.Model(&model.AdminRecoveryCode{}).
			Where("user_id = ? AND generation = ? AND verifier = ? AND used_at = 0", userID, factor.RecoveryGeneration, verifier).
			Update("used_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return common.NewError("invalid recovery code")
		}
		return nil
	})
}

func (s *MFAService) RotateRecoveryCodes(userID uint) ([]string, error) {
	now := s.now()
	var factor model.AdminMFAFactor
	if err := dbsqlite.DB().Model(&model.AdminMFAFactor{}).
		Where("user_id = ? AND state = ?", userID, MFAStateActive).
		First(&factor).Error; err != nil {
		return nil, common.NewError("MFA is not enabled")
	}
	codes, rows, generation, err := s.generateRecoveryRows(userID, factor.RecoveryGeneration+1, now)
	if err != nil {
		return nil, err
	}
	err = dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND generation = ?", userID, generation).
			Delete(&model.AdminRecoveryCode{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		result := tx.Model(&model.AdminMFAFactor{}).Where("user_id = ? AND recovery_generation = ?", userID, factor.RecoveryGeneration).
			Updates(map[string]any{
				"state":                       MFAStateActiveRecoveryAck,
				"pending_recovery_generation": generation,
				"pending_expires_at":          now.Add(MFAPendingTTL).Unix(),
				"recovery_acknowledged":       false,
				"updated_at":                  now.Unix(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return common.NewError("MFA factor changed")
		}
		return nil
	})
	return codes, err
}

func (s *MFAService) Disable(userID uint) error {
	return dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.AdminRecoveryCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.AdminMFAFactor{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.User{}).Where("id = ?", userID).
			Update("mfa_generation", gorm.Expr("mfa_generation + 1")).Error
	})
}

func (s *MFAService) VerifyPassword(ctx context.Context, userID uint, plaintext string) error {
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", userID).First(&user).Error; err != nil {
		_ = passwordutil.EqualizeUnknown(ctx, plaintext)
		return common.NewError("wrong password")
	}
	valid, _, err := passwordutil.Verify(ctx, user.Password, plaintext)
	if err != nil || !valid {
		return common.NewError("wrong password")
	}
	return nil
}

func (s *MFAService) generateRecoveryRows(userID uint, generation uint64, now time.Time) ([]string, []model.AdminRecoveryCode, uint64, error) {
	if generation == 0 {
		generation = 1
	}
	codes := make([]string, 0, RecoveryCodeCount)
	rows := make([]model.AdminRecoveryCode, 0, RecoveryCodeCount)
	for len(codes) < RecoveryCodeCount {
		raw := make([]byte, RecoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, 0, err
		}
		code := recoveryBase32.EncodeToString(raw)
		verifier, err := s.recoveryVerifier(userID, code)
		if err != nil {
			return nil, nil, 0, err
		}
		codes = append(codes, formatRecoveryCode(code))
		rows = append(rows, model.AdminRecoveryCode{
			UserID:     userID,
			Generation: generation,
			Verifier:   verifier,
			CreatedAt:  now.Unix(),
		})
	}
	return codes, rows, generation, nil
}

func (s *MFAService) recoveryVerifier(userID uint, code string) (string, error) {
	normalized := normalizeRecoveryCode(code)
	raw, err := recoveryBase32.DecodeString(normalized)
	if err != nil || len(raw) != RecoveryCodeBytes {
		return "", errors.New("invalid recovery code")
	}
	secret, err := s.SettingService.GetSecret()
	if err != nil {
		return "", err
	}
	salt, err := s.SettingService.GetInstallSalt()
	if err != nil {
		return "", err
	}
	key, err := settingscrypto.DeriveHKDFKey(secret, salt, []byte("sui:admin-recovery-code:v1"))
	if err != nil {
		return "", err
	}
	defer common.WipeBytes(key)
	mac := hmac.New(sha256.New, key)
	_, _ = fmt.Fprintf(mac, "%d:", userID)
	_, _ = mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func mfaSecretKey(userID uint) string {
	return "adminMFAFactor:" + strconv.FormatUint(uint64(userID), 10)
}

func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func formatRecoveryCode(code string) string {
	var parts []string
	for len(code) > 4 {
		parts = append(parts, code[:4])
		code = code[4:]
	}
	if code != "" {
		parts = append(parts, code)
	}
	return strings.Join(parts, "-")
}

func activeMFAStates() []string {
	return []string{
		MFAStateActive,
		MFAStateActivePendingRotation,
		MFAStateActiveRotationAck,
		MFAStateActiveRecoveryAck,
	}
}

func isActiveMFAState(state string) bool {
	for _, active := range activeMFAStates() {
		if state == active {
			return true
		}
	}
	return false
}

func isMFAAcknowledgmentState(state string) bool {
	return state == MFAStatePendingAcknowledgment ||
		state == MFAStateActiveRotationAck ||
		state == MFAStateActiveRecoveryAck
}
