package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

const StepUpTTL = 5 * time.Minute

const (
	stepUpSessionRefMaxBytes = 256
	stepUpRevisionMaxBytes   = 128
	stepUpOperationMaxBytes  = 128
)

type StepUpBinding struct {
	UserID                    uint
	SessionRef                string
	SessionGenerationRevision string
	CredentialGeneration      uint64
	MFAGeneration             uint64
	ClientIdentityRevision    string
	OperationKind             string
	TargetDigest              string
	Assurance                 string
}

type IssuedStepUpGrant struct {
	Token     string `json:"token"`
	Revision  string `json:"revision"`
	ExpiresAt int64  `json:"expiresAt"`
}

type StepUpService struct {
	SettingService SettingService
	Now            func() time.Time
}

func (s *StepUpService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *StepUpService) Issue(binding StepUpBinding) (IssuedStepUpGrant, error) {
	if err := validateStepUpBinding(binding); err != nil {
		return IssuedStepUpGrant{}, err
	}
	token, err := common.SecureRandom(48)
	if err != nil {
		return IssuedStepUpGrant{}, err
	}
	digest, err := s.digest(token)
	if err != nil {
		return IssuedStepUpGrant{}, err
	}
	now := s.now()
	revision, err := common.SecureRandom(24)
	if err != nil {
		return IssuedStepUpGrant{}, err
	}
	grant := model.StepUpGrant{
		Digest:                    digest,
		Revision:                  revision,
		UserID:                    binding.UserID,
		SessionRef:                binding.SessionRef,
		SessionGenerationRevision: binding.SessionGenerationRevision,
		CredentialGeneration:      binding.CredentialGeneration,
		MFAGeneration:             binding.MFAGeneration,
		OperationKind:             binding.OperationKind,
		TargetDigest:              boundStepUpTargetDigest(binding.TargetDigest, binding.ClientIdentityRevision),
		Assurance:                 binding.Assurance,
		CreatedAt:                 now.Unix(),
		ExpiresAt:                 now.Add(StepUpTTL).Unix(),
	}
	if err := dbsqlite.DB().Create(&grant).Error; err != nil {
		return IssuedStepUpGrant{}, err
	}
	return IssuedStepUpGrant{Token: token, Revision: grant.Revision, ExpiresAt: grant.ExpiresAt}, nil
}

// Consume atomically accepts a grant once and only for the exact durable
// identity, session, credential/MFA generations, operation, and target.
func (s *StepUpService) Consume(token string, binding StepUpBinding) error {
	if err := validateStepUpBinding(binding); err != nil {
		return err
	}
	digest, err := s.digest(token)
	if err != nil {
		return common.NewError("invalid step-up grant")
	}
	result := dbsqlite.DB().Model(&model.StepUpGrant{}).
		Where(
			"digest = ? AND user_id = ? AND session_ref = ? AND session_generation_revision = ? AND credential_generation = ? AND mfa_generation = ? AND operation_kind = ? AND target_digest = ? AND assurance = ? AND expires_at > ? AND consumed_at = 0",
			digest,
			binding.UserID,
			binding.SessionRef,
			binding.SessionGenerationRevision,
			binding.CredentialGeneration,
			binding.MFAGeneration,
			binding.OperationKind,
			boundStepUpTargetDigest(binding.TargetDigest, binding.ClientIdentityRevision),
			binding.Assurance,
			s.now().Unix(),
		).
		Update("consumed_at", s.now().Unix())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return common.NewError("invalid or expired step-up grant")
	}
	return nil
}

func (s *StepUpService) DeleteExpired() (int64, error) {
	result := dbsqlite.DB().Where("expires_at <= ? OR consumed_at > 0", s.now().Unix()).Delete(&model.StepUpGrant{})
	return result.RowsAffected, result.Error
}

func (s *StepUpService) InvalidateUser(userID uint) error {
	if userID == 0 {
		return nil
	}
	return dbsqlite.DB().Where("user_id = ?", userID).Delete(&model.StepUpGrant{}).Error
}

func (s *StepUpService) InvalidateSession(sessionRef string) error {
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return nil
	}
	return dbsqlite.DB().Where("session_ref = ?", sessionRef).Delete(&model.StepUpGrant{}).Error
}

func (s *StepUpService) digest(token string) (string, error) {
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 256 {
		return "", common.NewError("invalid step-up grant")
	}
	secret, err := s.SettingService.GetSecret()
	if err != nil {
		return "", err
	}
	salt, err := s.SettingService.GetInstallSalt()
	if err != nil {
		return "", err
	}
	key, err := settingscrypto.DeriveHKDFKey(secret, salt, []byte("sui:step-up-grant:v1"))
	if err != nil {
		return "", err
	}
	defer common.WipeBytes(key)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func validateStepUpBinding(binding StepUpBinding) error {
	sessionRef := strings.TrimSpace(binding.SessionRef)
	revision := strings.TrimSpace(binding.SessionGenerationRevision)
	operation := strings.TrimSpace(binding.OperationKind)
	targetDigest := strings.TrimSpace(binding.TargetDigest)
	clientIdentityRevision := strings.TrimSpace(binding.ClientIdentityRevision)
	assurance := strings.TrimSpace(binding.Assurance)
	if binding.UserID == 0 || sessionRef == "" || revision == "" || operation == "" ||
		binding.CredentialGeneration == 0 ||
		binding.MFAGeneration == 0 || clientIdentityRevision == "" {
		return common.NewError("invalid step-up binding")
	}
	if len(sessionRef) > stepUpSessionRefMaxBytes || len(revision) > stepUpRevisionMaxBytes || len(operation) > stepUpOperationMaxBytes {
		return common.NewError("invalid step-up binding")
	}
	digestBytes, err := hex.DecodeString(targetDigest)
	if err != nil || len(digestBytes) != sha256.Size || targetDigest != strings.ToLower(targetDigest) {
		return common.NewError("invalid step-up binding")
	}
	identityBytes, err := hex.DecodeString(clientIdentityRevision)
	if err != nil || len(identityBytes) != sha256.Size || clientIdentityRevision != strings.ToLower(clientIdentityRevision) {
		return common.NewError("invalid step-up binding")
	}
	switch assurance {
	case AssurancePassword, AssuranceMFA, AssuranceRecovery:
	default:
		return common.NewError("invalid step-up binding")
	}
	return nil
}

// boundStepUpTargetDigest keeps the durable schema unchanged while binding the
// persisted target to the exact request-side client/proxy authority that
// issued the grant. A change to either dimension produces an unrelated digest.
func boundStepUpTargetDigest(targetDigest, clientIdentityRevision string) string {
	sum := sha256.Sum256([]byte("step-up-target-client-binding-v1\n" + targetDigest + "\n" + clientIdentityRevision + "\n"))
	return hex.EncodeToString(sum[:])
}
