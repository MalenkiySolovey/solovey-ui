package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/util/password"
	totputil "github.com/MalenkiySolovey/solovey-ui/util/totp"
	"gorm.io/gorm"
)

func TestSessionLifetimePolicyRequiresExplicitLegacyAdoption(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Where("key = ?", "sessionLifetimePolicy").Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}

	legacy, err := settingService.ResolveSessionLifetime()
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Posture != LifetimePostureLegacyUnbounded || legacy.LegacyMaxAge != 0 {
		t.Fatalf("missing upgrade marker resolved to %#v", legacy)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "sessionMaxAge").Update("value", "7").Error; err != nil {
		t.Fatal(err)
	}
	configured, err := settingService.ResolveSessionLifetime()
	if err != nil {
		t.Fatal(err)
	}
	if configured.Posture != LifetimePostureLegacyExplicit || configured.LegacyMaxAge != 7*time.Minute {
		t.Fatalf("configured legacy policy resolved to %#v", configured)
	}
	if err := settingService.AdoptBoundedSessionLifetime(); err != nil {
		t.Fatal(err)
	}
	bounded, err := settingService.ResolveSessionLifetime()
	if err != nil {
		t.Fatal(err)
	}
	if bounded.Posture != LifetimePostureBoundedV1 || bounded.LegacyMaxAge != 0 {
		t.Fatalf("adopted policy resolved to %#v", bounded)
	}
}

func TestSettingsResetKeepsCurrentBoundedSessionDefault(t *testing.T) {
	settingService := initSettingTestDB(t)
	if err := settingService.ResetSettings(); err != nil {
		t.Fatal(err)
	}
	lifetime, err := settingService.ResolveSessionLifetime()
	if err != nil {
		t.Fatal(err)
	}
	if lifetime.Posture != LifetimePostureBoundedV1 {
		t.Fatalf("settings reset restored unsafe session posture: %#v", lifetime)
	}
}

func TestSecuritySessionCleanupIsBoundedAndRetainsRecentHistory(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0)
	rows := make([]model.SecuritySession, 0, MaxSessionCleanupBatch+2)
	for i := 0; i < MaxSessionCleanupBatch+1; i++ {
		rows = append(rows, model.SecuritySession{
			SessionID:  "stale-session-" + fmt.Sprint(i),
			Ref:        "stale-ref-" + fmt.Sprint(i),
			UserID:     admin.Id,
			RevokedAt:  now.Add(-sessionHistoryRetention - time.Hour).Unix(),
			LastSeenAt: now.Add(-sessionHistoryRetention - time.Hour).Unix(),
		})
	}
	rows = append(rows, model.SecuritySession{
		SessionID:  "recent-session",
		Ref:        "recent-ref",
		UserID:     admin.Id,
		RevokedAt:  now.Add(-time.Hour).Unix(),
		LastSeenAt: now.Add(-time.Hour).Unix(),
	})
	if err := dbsqlite.DB().Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	sessions := SecuritySessionService{Now: func() time.Time { return now }}
	deleted, err := sessions.CleanupExpired(MaxSessionCleanupBatch)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != MaxSessionCleanupBatch {
		t.Fatalf("cleanup deleted %d rows, want %d", deleted, MaxSessionCleanupBatch)
	}
	var stale, recent int64
	if err := dbsqlite.DB().Model(&model.SecuritySession{}).Where("ref LIKE 'stale-ref-%'").Count(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.SecuritySession{}).Where("ref = ?", "recent-ref").Count(&recent).Error; err != nil {
		t.Fatal(err)
	}
	if stale != 1 || recent != 1 {
		t.Fatalf("bounded cleanup left stale=%d recent=%d, want 1/1", stale, recent)
	}
}

func TestMFALifecycleEncryptsSecretPreventsReplayAndConsumesRecoveryOnce(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	mfa := MFAService{SettingService: *settingService, Now: func() time.Time { return now }}

	enrollment, err := mfa.BeginEnrollment(admin.Id, admin.Username)
	if err != nil {
		t.Fatal(err)
	}
	if enrollment.Secret == "" || !strings.HasPrefix(enrollment.URI, "otpauth://totp/") {
		t.Fatalf("invalid enrollment: %#v", enrollment)
	}
	var pending model.AdminMFAFactor
	if err := dbsqlite.DB().Where("user_id = ?", admin.Id).First(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.PendingSecretCiphertext == enrollment.Secret ||
		strings.Contains(pending.PendingSecretCiphertext, enrollment.Secret) {
		t.Fatal("pending TOTP secret was persisted in plaintext")
	}

	rawSecret, err := totputil.DecodeSecret(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	counter := uint64(now.Unix() / int64(totputil.Period/time.Second))
	code := totputil.Code(rawSecret, counter, totputil.Digits)
	recoveryCodes, err := mfa.ConfirmEnrollment(admin.Id, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(recoveryCodes) != RecoveryCodeCount {
		t.Fatalf("recovery codes=%d, want %d", len(recoveryCodes), RecoveryCodeCount)
	}
	var factor model.AdminMFAFactor
	if err := dbsqlite.DB().Where("user_id = ?", admin.Id).First(&factor).Error; err != nil {
		t.Fatal(err)
	}
	if factor.State != MFAStatePendingAcknowledgment || factor.ActiveSecretCiphertext != "" ||
		factor.PendingSecretCiphertext == "" || factor.RecoveryAcknowledged ||
		factor.PendingRecoveryGeneration == 0 {
		t.Fatalf("unexpected confirmed factor: %#v", factor)
	}
	var rows []model.AdminRecoveryCode
	if err := dbsqlite.DB().Where("user_id = ?", admin.Id).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != RecoveryCodeCount {
		t.Fatalf("stored recovery verifiers=%d, want %d", len(rows), RecoveryCodeCount)
	}
	for _, row := range rows {
		for _, plaintext := range recoveryCodes {
			if strings.Contains(row.Verifier, strings.ReplaceAll(plaintext, "-", "")) {
				t.Fatal("recovery-code plaintext leaked into verifier")
			}
		}
	}
	status, err := mfa.Status(admin.Id)
	if err != nil {
		t.Fatal(err)
	}
	if status.Enabled || !status.AwaitingAcknowledgment {
		t.Fatalf("MFA activated before recovery acknowledgement: %#v", status)
	}
	if err := mfa.AcknowledgeRecoveryCodes(admin.Id); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Where("user_id = ?", admin.Id).First(&factor).Error; err != nil {
		t.Fatal(err)
	}
	if factor.State != MFAStateActive || factor.ActiveSecretCiphertext == "" ||
		factor.PendingSecretCiphertext != "" || !factor.RecoveryAcknowledged {
		t.Fatalf("acknowledged factor was not promoted: %#v", factor)
	}
	if err := mfa.VerifyTOTP(admin.Id, code); err == nil {
		t.Fatal("same TOTP counter was accepted twice")
	}
	now = now.Add(totputil.Period)
	nextCode := totputil.Code(rawSecret, counter+1, totputil.Digits)
	if err := mfa.VerifyTOTP(admin.Id, nextCode); err != nil {
		t.Fatalf("next TOTP counter rejected: %v", err)
	}
	now = now.Add(totputil.Period)
	concurrentTOTP := totputil.Code(rawSecret, counter+2, totputil.Digits)
	if successes := concurrentMFAAttempts(2, func() error {
		return mfa.VerifyTOTP(admin.Id, concurrentTOTP)
	}); successes != 1 {
		t.Fatalf("concurrent TOTP successes=%d, want 1", successes)
	}

	if err := mfa.ConsumeRecoveryCode(admin.Id, recoveryCodes[0]); err != nil {
		t.Fatalf("first recovery-code use: %v", err)
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, recoveryCodes[0]); err == nil {
		t.Fatal("recovery code was accepted twice")
	}
	if successes := concurrentMFAAttempts(2, func() error {
		return mfa.ConsumeRecoveryCode(admin.Id, recoveryCodes[1])
	}); successes != 1 {
		t.Fatalf("concurrent recovery-code successes=%d, want 1", successes)
	}
	status, err = mfa.Status(admin.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || !status.RecoveryAcknowledged || status.RecoveryRemaining != RecoveryCodeCount-2 {
		t.Fatalf("unexpected MFA status: %#v", status)
	}
}

func TestMFAEnrollmentExpiryCannotActivateFactor(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	mfa := MFAService{SettingService: *settingService, Now: func() time.Time { return now }}
	enrollment, err := mfa.BeginEnrollment(admin.Id, admin.Username)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := totputil.DecodeSecret(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(MFAPendingTTL + time.Second)
	code := totputil.Code(raw, uint64(now.Unix()/int64(totputil.Period/time.Second)), totputil.Digits)
	if _, err := mfa.ConfirmEnrollment(admin.Id, code); err == nil {
		t.Fatal("expired enrollment was activated")
	}
	status, err := mfa.Status(admin.Id)
	if err != nil {
		t.Fatal(err)
	}
	if status.Enabled || status.Pending || status.AwaitingAcknowledgment {
		t.Fatalf("expired enrollment has active posture: %#v", status)
	}
}

func TestMFAMalformedCiphertextFailsClosedWithoutDisablingFactor(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	factor := model.AdminMFAFactor{
		UserID:                 admin.Id,
		State:                  MFAStateActive,
		ActiveSecretCiphertext: "secretbox:v1:malformed",
		RecoveryAcknowledged:   true,
		RecoveryGeneration:     1,
	}
	if err := dbsqlite.DB().Create(&factor).Error; err != nil {
		t.Fatal(err)
	}
	mfa := MFAService{SettingService: *settingService}
	if err := mfa.VerifyTOTP(admin.Id, "123456"); err == nil {
		t.Fatal("malformed encrypted authority was accepted")
	}
	status, err := mfa.Status(admin.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled {
		t.Fatalf("authority loss silently disabled MFA: %#v", status)
	}
	var persisted model.AdminMFAFactor
	if err := dbsqlite.DB().Where("user_id = ?", admin.Id).First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.State != MFAStateActive || persisted.ActiveSecretCiphertext != factor.ActiveSecretCiphertext {
		t.Fatalf("malformed factor was silently mutated: %#v", persisted)
	}
}

func TestAuthenticateRequiresMFAForEveryActiveRotationState(t *testing.T) {
	initSettingTestDB(t)
	const password = "Current administrator secret 2026!"
	if err := (&UserService{}).UpdateFirstUser("admin", password); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	factor := model.AdminMFAFactor{
		UserID:                 admin.Id,
		ActiveSecretCiphertext: "encrypted-authority-placeholder",
		State:                  MFAStateActive,
	}
	if err := dbsqlite.DB().Create(&factor).Error; err != nil {
		t.Fatal(err)
	}
	for _, state := range activeMFAStates() {
		if err := dbsqlite.DB().Model(&model.AdminMFAFactor{}).
			Where("user_id = ?", admin.Id).
			Update("state", state).Error; err != nil {
			t.Fatal(err)
		}
		result, err := (&UserService{}).Authenticate(t.Context(), "admin", password, "198.51.100.10")
		if err != nil {
			t.Fatalf("state %q authentication failed: %v", state, err)
		}
		if result.AuthState != AuthStateMFAPending {
			t.Fatalf("state %q bypassed MFA with auth state %q", state, result.AuthState)
		}
	}
	if err := dbsqlite.DB().Model(&model.AdminMFAFactor{}).
		Where("user_id = ?", admin.Id).
		Update("state", MFAStatePendingAcknowledgment).Error; err != nil {
		t.Fatal(err)
	}
	result, err := (&UserService{}).Authenticate(t.Context(), "admin", password, "198.51.100.10")
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthState != AuthStateAuthenticated {
		t.Fatalf("unacknowledged first enrollment unexpectedly required MFA: %q", result.AuthState)
	}
}

func TestMFARotationKeepsOldFactorUntilRecoveryAcknowledgment(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	mfa, oldSecret, oldRecovery := activateTestMFA(t, settingService, admin, &now)
	var before model.AdminMFAFactor
	if err := dbsqlite.DB().Where("user_id = ?", admin.Id).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	var userBefore model.User
	if err := dbsqlite.DB().First(&userBefore, admin.Id).Error; err != nil {
		t.Fatal(err)
	}

	now = now.Add(totputil.Period)
	replacement, err := mfa.BeginEnrollment(admin.Id, admin.Username)
	if err != nil {
		t.Fatal(err)
	}
	replacementRaw, err := totputil.DecodeSecret(replacement.Secret)
	if err != nil {
		t.Fatal(err)
	}
	replacementCounter := uint64(now.Unix() / int64(totputil.Period/time.Second))
	replacementCode := totputil.Code(replacementRaw, replacementCounter, totputil.Digits)
	replacementRecovery, err := mfa.ConfirmEnrollment(admin.Id, replacementCode)
	if err != nil {
		t.Fatal(err)
	}
	var staged model.AdminMFAFactor
	if err := dbsqlite.DB().Where("user_id = ?", admin.Id).First(&staged).Error; err != nil {
		t.Fatal(err)
	}
	if staged.State != MFAStateActiveRotationAck ||
		staged.ActiveSecretCiphertext != before.ActiveSecretCiphertext ||
		staged.PendingSecretCiphertext == "" {
		t.Fatalf("old factor was not preserved during staged rotation: %#v", staged)
	}
	var userStaged model.User
	if err := dbsqlite.DB().First(&userStaged, admin.Id).Error; err != nil {
		t.Fatal(err)
	}
	if userStaged.MFAGeneration != userBefore.MFAGeneration {
		t.Fatal("MFA generation changed before recovery acknowledgement")
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, oldRecovery[0]); err != nil {
		t.Fatalf("old recovery set stopped before acknowledgement: %v", err)
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, replacementRecovery[0]); err == nil {
		t.Fatal("staged recovery code was active before acknowledgement")
	}

	if err := mfa.AcknowledgeRecoveryCodes(admin.Id); err != nil {
		t.Fatal(err)
	}
	var promoted model.AdminMFAFactor
	if err := dbsqlite.DB().Where("user_id = ?", admin.Id).First(&promoted).Error; err != nil {
		t.Fatal(err)
	}
	if promoted.State != MFAStateActive ||
		promoted.ActiveSecretCiphertext == before.ActiveSecretCiphertext ||
		promoted.PendingSecretCiphertext != "" {
		t.Fatalf("replacement factor was not promoted: %#v", promoted)
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, oldRecovery[1]); err == nil {
		t.Fatal("old recovery code survived acknowledged rotation")
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, replacementRecovery[0]); err != nil {
		t.Fatalf("promoted recovery code rejected: %v", err)
	}
	now = now.Add(totputil.Period)
	oldRaw, err := totputil.DecodeSecret(oldSecret)
	if err != nil {
		t.Fatal(err)
	}
	counter := uint64(now.Unix() / int64(totputil.Period/time.Second))
	if err := mfa.VerifyTOTP(admin.Id, totputil.Code(oldRaw, counter, totputil.Digits)); err == nil {
		t.Fatal("old authenticator survived acknowledged rotation")
	}
	if err := mfa.VerifyTOTP(admin.Id, totputil.Code(replacementRaw, counter, totputil.Digits)); err != nil {
		t.Fatalf("replacement authenticator rejected: %v", err)
	}

	rotatedRecovery, err := mfa.RotateRecoveryCodes(admin.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, replacementRecovery[1]); err != nil {
		t.Fatalf("current recovery set stopped during staged regeneration: %v", err)
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, rotatedRecovery[0]); err == nil {
		t.Fatal("regenerated recovery code was active before acknowledgement")
	}
	if err := mfa.AcknowledgeRecoveryCodes(admin.Id); err != nil {
		t.Fatal(err)
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, replacementRecovery[2]); err == nil {
		t.Fatal("previous recovery set survived regeneration acknowledgement")
	}
	if err := mfa.ConsumeRecoveryCode(admin.Id, rotatedRecovery[0]); err != nil {
		t.Fatalf("acknowledged regenerated recovery code rejected: %v", err)
	}
}

func TestRecoveryTransitionRotatesCredentialAndDisablesLostMFAAtomically(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	mfa, _, recoveryCodes := activateTestMFA(t, settingService, admin, &now)
	if err := mfa.ConsumeRecoveryCode(admin.Id, recoveryCodes[0]); err != nil {
		t.Fatal(err)
	}
	var before model.User
	if err := dbsqlite.DB().First(&before, admin.Id).Error; err != nil {
		t.Fatal(err)
	}
	stepUp := StepUpService{SettingService: *settingService, Now: func() time.Time { return now }}
	if _, err := stepUp.Issue(StepUpBinding{
		UserID: admin.Id, SessionRef: "recovery-session",
		SessionGenerationRevision: "generation-revision",
		CredentialGeneration:      nonzeroGeneration(before.CredentialGeneration),
		MFAGeneration:             nonzeroGeneration(before.MFAGeneration),
		ClientIdentityRevision:    strings.Repeat("c", 64),
		OperationKind:             "mfa.disable",
		TargetDigest:              strings.Repeat("a", 64),
		Assurance:                 AssuranceRecovery,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := (&UserService{}).CompleteRecoveryTransition(
		context.Background(),
		admin.Id,
		"recovered-admin",
		"Recovered account secret 2026 · delta",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.CredentialGeneration != nonzeroGeneration(before.CredentialGeneration)+1 ||
		result.MFAGeneration != nonzeroGeneration(before.MFAGeneration)+1 {
		t.Fatalf("security generations were not rotated: %#v", result)
	}
	var recovered model.User
	if err := dbsqlite.DB().Where("username = ?", "recovered-admin").First(&recovered).Error; err != nil {
		t.Fatal(err)
	}
	valid, _, err := passwordutil.Verify(context.Background(), recovered.Password, "Recovered account secret 2026 · delta")
	if err != nil || !valid || recovered.ForcePasswordReset {
		t.Fatalf("recovered credential invalid: valid=%v err=%v user=%#v", valid, err, recovered)
	}
	for name, value := range map[string]any{
		"factor":   &model.AdminMFAFactor{},
		"recovery": &model.AdminRecoveryCode{},
		"step-up":  &model.StepUpGrant{},
	} {
		var count int64
		if err := dbsqlite.DB().Model(value).Where("user_id = ?", admin.Id).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s state survived recovery transition: %d", name, count)
		}
	}
}

func TestStepUpGrantExactBindingSingleUseAndExpiry(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	stepUp := StepUpService{SettingService: *settingService, Now: func() time.Time { return now }}
	binding := StepUpBinding{
		UserID:                    1,
		SessionRef:                "session-reference",
		SessionGenerationRevision: "session-generation-revision",
		CredentialGeneration:      3,
		MFAGeneration:             4,
		ClientIdentityRevision:    strings.Repeat("c", 64),
		OperationKind:             "mfa.disable",
		TargetDigest:              strings.Repeat("a", 64),
		Assurance:                 AssuranceMFA,
	}
	grant, err := stepUp.Issue(binding)
	if err != nil {
		t.Fatal(err)
	}
	changed := binding
	changed.TargetDigest = strings.Repeat("b", 64)
	if err := stepUp.Consume(grant.Token, changed); err == nil {
		t.Fatal("grant accepted a different target")
	}
	changed = binding
	changed.ClientIdentityRevision = strings.Repeat("d", 64)
	if err := stepUp.Consume(grant.Token, changed); err == nil {
		t.Fatal("grant accepted a different client/proxy identity")
	}
	if err := stepUp.Consume(grant.Token, binding); err != nil {
		t.Fatalf("exact grant rejected: %v", err)
	}
	if err := stepUp.Consume(grant.Token, binding); err == nil {
		t.Fatal("step-up grant was consumed twice")
	}

	concurrent, err := stepUp.Issue(binding)
	if err != nil {
		t.Fatal(err)
	}
	if successes := concurrentMFAAttempts(2, func() error {
		return stepUp.Consume(concurrent.Token, binding)
	}); successes != 1 {
		t.Fatalf("concurrent step-up successes=%d, want 1", successes)
	}

	expired, err := stepUp.Issue(binding)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(StepUpTTL + time.Second)
	if err := stepUp.Consume(expired.Token, binding); err == nil {
		t.Fatal("expired step-up grant was accepted")
	}
	var plaintextCount int64
	if err := dbsqlite.DB().Model(&model.StepUpGrant{}).Where("digest = ?", expired.Token).Count(&plaintextCount).Error; err != nil {
		t.Fatal(err)
	}
	if plaintextCount != 0 {
		t.Fatal("step-up plaintext token was persisted")
	}
}

func TestStepUpGrantRejectsIncompleteOrUnboundedBindings(t *testing.T) {
	valid := StepUpBinding{
		UserID:                    1,
		SessionRef:                "session-reference",
		SessionGenerationRevision: "session-generation-revision",
		CredentialGeneration:      3,
		MFAGeneration:             4,
		ClientIdentityRevision:    strings.Repeat("c", 64),
		OperationKind:             "mfa.disable",
		TargetDigest:              strings.Repeat("a", 64),
		Assurance:                 AssuranceMFA,
	}
	tests := map[string]StepUpBinding{
		"missing revision": func() StepUpBinding { changed := valid; changed.SessionGenerationRevision = ""; return changed }(),
		"oversized ref": func() StepUpBinding {
			changed := valid
			changed.SessionRef = strings.Repeat("x", stepUpSessionRefMaxBytes+1)
			return changed
		}(),
		"malformed digest":        func() StepUpBinding { changed := valid; changed.TargetDigest = strings.Repeat("z", 64); return changed }(),
		"missing client identity": func() StepUpBinding { changed := valid; changed.ClientIdentityRevision = ""; return changed }(),
		"malformed client identity": func() StepUpBinding {
			changed := valid
			changed.ClientIdentityRevision = strings.Repeat("z", 64)
			return changed
		}(),
		"unknown assurance": func() StepUpBinding { changed := valid; changed.Assurance = "unknown"; return changed }(),
	}
	for name, binding := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateStepUpBinding(binding); err == nil {
				t.Fatal("invalid step-up binding was accepted")
			}
		})
	}
}

func concurrentMFAAttempts(count int, attempt func() error) int {
	start := make(chan struct{})
	results := make(chan error, count)
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- attempt()
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	return successes
}

func activateTestMFA(
	t *testing.T,
	settingService *SettingService,
	admin model.User,
	now *time.Time,
) (MFAService, string, []string) {
	t.Helper()
	mfa := MFAService{SettingService: *settingService, Now: func() time.Time { return *now }}
	enrollment, err := mfa.BeginEnrollment(admin.Id, admin.Username)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := totputil.DecodeSecret(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	counter := uint64(now.Unix() / int64(totputil.Period/time.Second))
	codes, err := mfa.ConfirmEnrollment(admin.Id, totputil.Code(raw, counter, totputil.Digits))
	if err != nil {
		t.Fatal(err)
	}
	if err := mfa.AcknowledgeRecoveryCodes(admin.Id); err != nil {
		t.Fatal(err)
	}
	return mfa, enrollment.Secret, codes
}

func TestSecuritySessionRevocationDeletesBearerAndClosesOnlyBoundRealtime(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	data BLOB NOT NULL,
	expires_at INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	for _, identity := range []struct {
		id  string
		ref string
	}{
		{id: "session-a", ref: "ref-a"},
		{id: "session-b", ref: "ref-b"},
	} {
		if err := dbsqlite.DB().Exec(
			"INSERT INTO sessions(id, data, expires_at) VALUES(?,?,?)",
			identity.id, []byte("opaque-server-data"), now.Add(time.Hour).Unix(),
		).Error; err != nil {
			t.Fatal(err)
		}
		row := model.SecuritySession{
			SessionID: identity.id, Ref: identity.ref, UserID: admin.Id,
			UsernameSnapshot: admin.Username, State: SessionStateActive,
			AuthState: AuthStateAuthenticated, Assurance: AssurancePassword,
			LifetimePosture:           LifetimePostureBoundedV1,
			SessionGenerationRevision: "generation-revision",
			CredentialGeneration:      nonzeroGeneration(admin.CredentialGeneration),
			MFAGeneration:             nonzeroGeneration(admin.MFAGeneration),
			CreatedAt:                 now.Unix(), AuthenticatedAt: now.Unix(), LastSeenAt: now.Unix(),
			IdleExpiresAt:     now.Add(DefaultSessionIdle).Unix(),
			AbsoluteExpiresAt: now.Add(DefaultSessionAbsolute).Unix(),
			ClientProvenance:  "direct", ClientPrefix: "198.51.100.0/24",
			UserAgentHash: UserAgentDigest("browser"), DeviceLabel: "browser",
		}
		if err := dbsqlite.DB().Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	realtime.CloseAll("test_reset")
	t.Cleanup(func() { realtime.CloseAll("test_done") })
	firstDrops := make(chan string, 1)
	secondDrops := make(chan string, 1)
	unregisterFirst := realtime.Register(&realtime.ClientHandle{
		User: admin.Username, SessionRef: "ref-a", Scope: realtime.ScopeAdmin,
		SendCh: make(chan realtime.Event, 1),
		OnDrop: func(reason string) {
			firstDrops <- reason
		},
	})
	defer unregisterFirst()
	unregisterSecond := realtime.Register(&realtime.ClientHandle{
		User: admin.Username, SessionRef: "ref-b", Scope: realtime.ScopeAdmin,
		SendCh: make(chan realtime.Event, 1),
		OnDrop: func(reason string) {
			secondDrops <- reason
		},
	})
	defer unregisterSecond()

	sessions := SecuritySessionService{Now: func() time.Time { return now }}
	if err := sessions.Revoke(admin.Id, "ref-a", "user_revoked"); err != nil {
		t.Fatal(err)
	}
	select {
	case reason := <-firstDrops:
		if reason != "session_revoked" {
			t.Fatalf("unexpected websocket close reason: %s", reason)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked session websocket was not closed")
	}
	select {
	case reason := <-secondDrops:
		t.Fatalf("unrelated session websocket was closed: %s", reason)
	case <-time.After(25 * time.Millisecond):
	}
	var bearerCount int64
	if err := dbsqlite.DB().Table("sessions").Where("id = ?", "session-a").Count(&bearerCount).Error; err != nil {
		t.Fatal(err)
	}
	if bearerCount != 0 {
		t.Fatalf("revoked bearer row remains: %d", bearerCount)
	}
	if _, err := sessions.Validate(
		"ref-a",
		admin.Id,
		nonzeroGeneration(admin.CredentialGeneration),
		nonzeroGeneration(admin.MFAGeneration),
	); err == nil {
		t.Fatal("revoked session still validates")
	}
}

func TestRevokeOthersDoesNotUpgradeAStaleSQLiteReadTransaction(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	data BLOB NOT NULL,
	expires_at INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	for _, session := range []model.SecuritySession{
		{SessionID: "current-session", Ref: "current-ref", UserID: admin.Id, State: SessionStateActive},
		{SessionID: "other-session", Ref: "other-ref", UserID: admin.Id, State: SessionStateActive},
	} {
		if err := dbsqlite.DB().Create(&session).Error; err != nil {
			t.Fatal(err)
		}
		if err := dbsqlite.DB().Exec(
			"INSERT INTO sessions(id, data, expires_at) VALUES(?,?,0)",
			session.SessionID, []byte("opaque"),
		).Error; err != nil {
			t.Fatal(err)
		}
	}

	db := dbsqlite.DB()
	callbackName := "p18:e02:concurrent-writer-after-session-read"
	writerDone := make(chan error, 1)
	fired := false
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if fired || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "security_sessions" {
			return
		}
		fired = true
		go func() {
			writerDone <- db.Create(&model.Setting{Key: "p18-e02-session-writer", Value: "committed"}).Error
		}()
		if err := <-writerDone; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	revoked, err := (&SecuritySessionService{}).RevokeOthers(admin.Id, "current-ref", "mfa_disabled")
	if err != nil {
		t.Fatalf("revoke others failed after concurrent WAL writer: %v", err)
	}
	if !fired || revoked != 1 {
		t.Fatalf("concurrency fixture or revocation result invalid: fired=%v revoked=%d", fired, revoked)
	}
	var currentCount, otherCount int64
	if err := db.Model(&model.SecuritySession{}).Where("ref = ? AND revoked_at = 0", "current-ref").Count(&currentCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("sessions").Where("id = ?", "other-session").Count(&otherCount).Error; err != nil {
		t.Fatal(err)
	}
	if currentCount != 1 || otherCount != 0 {
		t.Fatalf("revocation state current=%d other-backing=%d", currentCount, otherCount)
	}
}
