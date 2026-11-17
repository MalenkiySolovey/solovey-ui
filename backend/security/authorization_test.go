package security_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/mfa"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/stepup"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/totp"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"

	"gorm.io/gorm"
)

// opaqueSettings is a test implementation of the supplied secret port. Stored
// handles reveal no plaintext; encryption itself remains the settings owner's
// responsibility and is covered by the Product integration tests.
type opaqueSettings struct {
	values map[string]string
	next   int
}

func (s *opaqueSettings) EncryptString(key, value string) (string, error) {
	s.next++
	handle := fmt.Sprintf("opaque:%d", s.next)
	s.values[key+":"+handle] = value
	return handle, nil
}
func (s *opaqueSettings) DecryptString(key, value string) (string, error) {
	plaintext, ok := s.values[key+":"+value]
	if !ok {
		return "", errors.New("ciphertext is unavailable")
	}
	return plaintext, nil
}
func (*opaqueSettings) MasterSecret() ([]byte, error) { return []byte(strings.Repeat("m", 32)), nil }
func (*opaqueSettings) InstallSalt() ([]byte, error)  { return []byte(strings.Repeat("s", 32)), nil }

func initAuthorizationDB(t *testing.T) (*gorm.DB, uint) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "security.db")
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := dbsqlite.InitializeContext(context.Background(), path, dbsqlite.Bootstrap{AfterOpen: func(db *gorm.DB) error {
		return db.AutoMigrate(&model.User{}, &model.AdminMFAFactor{}, &model.AdminRecoveryCode{}, &model.StepUpGrant{})
	}}); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	user := model.User{Username: "owner-test", CredentialGeneration: 1, MFAGeneration: 1}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	return db, user.ID
}

func totpCode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	decoded, err := totp.DecodeSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	return totp.Code(decoded, uint64(now.Unix()/30), 6)
}

func concurrentSuccesses(count int, operation func() error) int32 {
	var accepted atomic.Int32
	var workers sync.WaitGroup
	ready := make(chan struct{})
	for range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-ready
			if operation() == nil {
				accepted.Add(1)
			}
		}()
	}
	close(ready)
	workers.Wait()
	return accepted.Load()
}

func TestMFAOwnerAcknowledgmentAndConcurrentReplayFences(t *testing.T) {
	db, userID := initAuthorizationDB(t)
	now := time.Date(2026, time.November, 17, 0, 0, 0, 0, time.UTC)
	secrets := &opaqueSettings{values: map[string]string{}}
	owner := mfa.Service{Settings: secrets, Now: func() time.Time { return now }}
	enrollment, err := owner.BeginEnrollment(userID, "owner-test")
	if err != nil {
		t.Fatal(err)
	}
	var factor model.AdminMFAFactor
	if err := db.Where("user_id=?", userID).First(&factor).Error; err != nil {
		t.Fatal(err)
	}
	if factor.PendingSecretCiphertext == "" || strings.Contains(factor.PendingSecretCiphertext, enrollment.Secret) {
		t.Fatal("enrollment persisted plaintext instead of opaque settings output")
	}
	codes, err := owner.ConfirmEnrollment(userID, totpCode(t, enrollment.Secret, now))
	if err != nil || len(codes) != mfa.RecoveryCodeCount {
		t.Fatalf("confirm = %d codes, %v", len(codes), err)
	}
	if err := owner.VerifyTOTP(userID, totpCode(t, enrollment.Secret, now)); err == nil {
		t.Fatal("unacknowledged enrollment authorized MFA")
	}
	if err := owner.AcknowledgeRecoveryCodes(userID); err != nil {
		t.Fatal(err)
	}
	if err := owner.VerifyTOTP(userID, totpCode(t, enrollment.Secret, now)); err == nil {
		t.Fatal("confirmation code replayed after acknowledgment")
	}
	now = now.Add(30 * time.Second)
	code := totpCode(t, enrollment.Secret, now)
	if got := concurrentSuccesses(12, func() error { return owner.VerifyTOTP(userID, code) }); got != 1 {
		t.Fatalf("same TOTP accepted %d times", got)
	}
	if got := concurrentSuccesses(12, func() error { return owner.ConsumeRecoveryCode(userID, codes[0]) }); got != 1 {
		t.Fatalf("same recovery code accepted %d times", got)
	}
	status, err := owner.Status(userID)
	if err != nil || !status.Enabled || status.RecoveryRemaining != mfa.RecoveryCodeCount-1 {
		t.Fatalf("MFA status after replay = %+v, %v", status, err)
	}
	var rows []model.AdminRecoveryCode
	if err := db.Where("user_id=?", userID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, plaintext := range codes {
			if row.Verifier == plaintext || strings.Contains(row.Verifier, strings.ReplaceAll(plaintext, "-", "")) {
				t.Fatal("recovery verifier persisted plaintext")
			}
		}
	}
	if err := db.Model(&model.AdminMFAFactor{}).Where("user_id=?", userID).Update("active_secret_ciphertext", "unreadable").Error; err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if err := owner.VerifyTOTP(userID, totpCode(t, enrollment.Secret, now)); err == nil {
		t.Fatal("unreadable secret authorized MFA")
	}
	status, err = owner.Status(userID)
	if err != nil || !status.Enabled {
		t.Fatalf("ciphertext failure disabled the factor: %+v, %v", status, err)
	}
}

func TestStepUpOwnerExactIdentityAndSingleUseBeforeEffect(t *testing.T) {
	db, userID := initAuthorizationDB(t)
	now := time.Date(2026, time.November, 17, 0, 0, 0, 0, time.UTC)
	owner := stepup.Service{Settings: &opaqueSettings{values: map[string]string{}}, Now: func() time.Time { return now }}
	digest := func(value string) string { sum := sha256.Sum256([]byte(value)); return hex.EncodeToString(sum[:]) }
	binding := stepup.Binding{UserID: userID, SessionRef: "bound-session", SessionGenerationRevision: "revision", CredentialGeneration: 1, MFAGeneration: 1, ClientIdentityRevision: digest("client"), OperationKind: "component.remove", TargetDigest: digest("component"), Assurance: panelsecurity.AssuranceMFA}
	grant, err := owner.Issue(binding)
	if err != nil {
		t.Fatal(err)
	}
	variants := []func(*stepup.Binding){
		func(b *stepup.Binding) { b.UserID++ }, func(b *stepup.Binding) { b.SessionRef = "other" }, func(b *stepup.Binding) { b.SessionGenerationRevision = "other" }, func(b *stepup.Binding) { b.CredentialGeneration++ }, func(b *stepup.Binding) { b.MFAGeneration++ }, func(b *stepup.Binding) { b.ClientIdentityRevision = digest("other-client") }, func(b *stepup.Binding) { b.OperationKind = "other-operation" }, func(b *stepup.Binding) { b.TargetDigest = digest("other-target") }, func(b *stepup.Binding) { b.Assurance = panelsecurity.AssurancePassword },
	}
	for i, change := range variants {
		wrong := binding
		change(&wrong)
		if err := owner.Consume(grant.Token, wrong); err == nil {
			t.Fatalf("wrong binding dimension %d consumed grant", i)
		}
	}
	var stored model.StepUpGrant
	if err := db.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ConsumedAt != 0 || stored.Digest == grant.Token || strings.Contains(stored.Digest, grant.Token) {
		t.Fatal("rejected binding consumed grant or persisted bearer plaintext")
	}
	var effects atomic.Int32
	accepted := concurrentSuccesses(16, func() error {
		if err := owner.Consume(grant.Token, binding); err != nil {
			return err
		}
		effects.Add(1)
		return nil
	})
	if accepted != 1 || effects.Load() != 1 {
		t.Fatalf("single-use acceptance/effects = %d/%d", accepted, effects.Load())
	}
	expired, err := owner.Issue(binding)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(stepup.TTL + time.Second)
	if err := owner.Consume(expired.Token, binding); err == nil {
		t.Fatal("expired grant authorized effect")
	}
	if effects.Load() != 1 {
		t.Fatal("rejection changed effects")
	}
}
