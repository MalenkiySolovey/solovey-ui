package stepup

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"time"

	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	backendsecuritycrypto "github.com/MalenkiySolovey/solovey-ui/backend/security/crypto"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
)

// SecretSettings supplies the narrow Product-owned secret operations required
// to derive and verify Step-Up grant digests.
type SecretSettings interface {
	MasterSecret() ([]byte, error)
	InstallSalt() ([]byte, error)
}

const TTL = 5 * time.Minute

const (
	SessionRefMaxBytes = 256
	RevisionMaxBytes   = 128
	OperationMaxBytes  = 128
)

type Binding struct {
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

type IssuedGrant struct {
	Token     string `json:"token"`
	Revision  string `json:"revision"`
	ExpiresAt int64  `json:"expiresAt"`
}

type Service struct {
	Settings SecretSettings
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) Issue(binding Binding) (IssuedGrant, error) {
	if err := ValidateBinding(binding); err != nil {
		return IssuedGrant{}, err
	}
	token, err := secureRandom(48)
	if err != nil {
		return IssuedGrant{}, err
	}
	digest, err := s.digest(token)
	if err != nil {
		return IssuedGrant{}, err
	}
	now := s.now()
	revision, err := secureRandom(24)
	if err != nil {
		return IssuedGrant{}, err
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
		ExpiresAt:                 now.Add(TTL).Unix(),
	}
	if err := dbsqlite.DB().Create(&grant).Error; err != nil {
		return IssuedGrant{}, err
	}
	return IssuedGrant{Token: token, Revision: grant.Revision, ExpiresAt: grant.ExpiresAt}, nil
}

// Consume atomically accepts a grant once and only for the exact durable
// identity, session, credential/MFA generations, operation, and target.
func (s *Service) Consume(token string, binding Binding) error {
	if err := ValidateBinding(binding); err != nil {
		return err
	}
	digest, err := s.digest(token)
	if err != nil {
		return errors.New("invalid step-up grant")
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
		return errors.New("invalid or expired step-up grant")
	}
	return nil
}

func (s *Service) DeleteExpired() (int64, error) {
	result := dbsqlite.DB().Where("expires_at <= ? OR consumed_at > 0", s.now().Unix()).Delete(&model.StepUpGrant{})
	return result.RowsAffected, result.Error
}

func (s *Service) InvalidateUser(userID uint) error {
	if userID == 0 {
		return nil
	}
	return dbsqlite.DB().Where("user_id = ?", userID).Delete(&model.StepUpGrant{}).Error
}

func (s *Service) InvalidateSession(sessionRef string) error {
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return nil
	}
	return dbsqlite.DB().Where("session_ref = ?", sessionRef).Delete(&model.StepUpGrant{}).Error
}

func (s *Service) digest(token string) (string, error) {
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 256 {
		return "", errors.New("invalid step-up grant")
	}
	secret, err := s.Settings.MasterSecret()
	if err != nil {
		return "", err
	}
	salt, err := s.Settings.InstallSalt()
	if err != nil {
		return "", err
	}
	key, err := backendsecuritycrypto.DeriveHKDFKey(secret, salt, []byte("sui:step-up-grant:v1"))
	if err != nil {
		return "", err
	}
	defer wipeBytes(key)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func ValidateBinding(binding Binding) error {
	sessionRef := strings.TrimSpace(binding.SessionRef)
	revision := strings.TrimSpace(binding.SessionGenerationRevision)
	operation := strings.TrimSpace(binding.OperationKind)
	targetDigest := strings.TrimSpace(binding.TargetDigest)
	clientIdentityRevision := strings.TrimSpace(binding.ClientIdentityRevision)
	assurance := strings.TrimSpace(binding.Assurance)
	if binding.UserID == 0 || sessionRef == "" || revision == "" || operation == "" ||
		binding.CredentialGeneration == 0 ||
		binding.MFAGeneration == 0 || clientIdentityRevision == "" {
		return errors.New("invalid step-up binding")
	}
	if len(sessionRef) > SessionRefMaxBytes || len(revision) > RevisionMaxBytes || len(operation) > OperationMaxBytes {
		return errors.New("invalid step-up binding")
	}
	digestBytes, err := hex.DecodeString(targetDigest)
	if err != nil || len(digestBytes) != sha256.Size || targetDigest != strings.ToLower(targetDigest) {
		return errors.New("invalid step-up binding")
	}
	identityBytes, err := hex.DecodeString(clientIdentityRevision)
	if err != nil || len(identityBytes) != sha256.Size || clientIdentityRevision != strings.ToLower(clientIdentityRevision) {
		return errors.New("invalid step-up binding")
	}
	switch assurance {
	case panelsecurity.AssurancePassword, panelsecurity.AssuranceMFA, panelsecurity.AssuranceRecovery:
	default:
		return errors.New("invalid step-up binding")
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

const secureRandomAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func secureRandom(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("secure random length is invalid")
	}
	result := make([]byte, length)
	maximum := big.NewInt(int64(len(secureRandomAlphabet)))
	for i := range result {
		value, err := rand.Int(rand.Reader, maximum)
		if err != nil {
			return "", err
		}
		result[i] = secureRandomAlphabet[value.Int64()]
	}
	return string(result), nil
}

func wipeBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
