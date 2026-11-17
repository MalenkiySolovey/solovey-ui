package security_test

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/accounts"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/password"
	"github.com/MalenkiySolovey/solovey-ui/backend/security/tokens"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
)

const initialAccountPassword = "Initial account credential for portable owner!"
const replacementAccountPassword = "Replacement account credential for portable owner!"

func initAccountCommandsDB(t *testing.T) (*gorm.DB, *accounts.Service, model.User) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "accounts.db")
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := dbsqlite.InitializeContext(context.Background(), file, dbsqlite.Bootstrap{
		BeforeOpen: func() error { return nil },
		AfterOpen: func(db *gorm.DB) error {
			return db.AutoMigrate(&model.User{}, &model.Tokens{}, &model.AdminMFAFactor{}, &model.AdminRecoveryCode{}, &model.StepUpGrant{})
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner := &accounts.Service{
		NextSortOrder: func(tx *gorm.DB) (int, error) {
			var maximum int
			err := tx.Model(&model.User{}).Select("COALESCE(MAX(sort_order), 0)").Scan(&maximum).Error
			return maximum + 1, err
		},
		ValidateImportName: func(name string) error {
			if name == "" {
				return errors.New("missing name")
			}
			return nil
		},
	}
	if err := owner.UpdateFirstUser("admin", initialAccountPassword); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	var user model.User
	if err := db.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	return db, owner, user
}

func TestAdministratorCredentialCleanupRunsAfterDurablePublication(t *testing.T) {
	db, owner, user := initAccountCommandsDB(t)
	if err := db.Model(&model.User{}).Where("id=?", user.Id).Update("force_password_reset", true).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	cleanupFailure := errors.New("fixture credential file is unavailable")
	owner.RemoveInitialCredential = func() error {
		calls++
		var stored model.User
		if err := db.First(&stored, user.Id).Error; err != nil {
			return err
		}
		if stored.ForcePasswordReset || stored.Username != "acknowledged-admin" || stored.CredentialGeneration != user.CredentialGeneration+1 {
			t.Error("cleanup ran before credential publication")
		}
		if valid, _, err := password.Verify(context.Background(), stored.Password, replacementAccountPassword); err != nil || !valid {
			t.Error("cleanup observed an unusable replacement credential")
		}
		return cleanupFailure
	}
	if _, err := owner.CompletePasswordTransition(context.Background(), user.Id, "wrong credential", "acknowledged-admin", replacementAccountPassword); err == nil {
		t.Fatal("incorrect current password authorized publication")
	}
	if calls != 0 {
		t.Fatal("rejected transition invoked credential cleanup")
	}
	result, err := owner.CompletePasswordTransition(context.Background(), user.Id, initialAccountPassword, "acknowledged-admin", replacementAccountPassword)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result.InitialCredentialRemoved || result.CredentialGeneration != user.CredentialGeneration+1 {
		t.Fatalf("cleanup failure altered committed result: calls=%d result=%#v", calls, result)
	}
}

func seedAccountSecurity(t *testing.T, db *gorm.DB, user model.User) {
	t.Helper()
	for _, row := range []any{
		&model.AdminMFAFactor{UserID: user.Id, State: "active", ActiveSecretCiphertext: "opaque-factor"},
		&model.AdminRecoveryCode{UserID: user.Id, Generation: 1, Verifier: "fixture-recovery-verifier"},
		&model.StepUpGrant{UserID: user.Id, Digest: "fixture-grant-verifier", SessionRef: "fixture-session"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestAdministratorRecoveryRollbackKeepsCredentialFactorAndGrantTogether(t *testing.T) {
	db, owner, user := initAccountCommandsDB(t)
	seedAccountSecurity(t, db, user)
	const callback = "test:reject-factor-removal"
	if err := db.Callback().Delete().Before("gorm:delete").Register(callback, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "admin_mfa_factors" {
			tx.AddError(errors.New("fixture factor removal failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Delete().Remove(callback) })
	if _, err := owner.CompleteRecoveryTransition(context.Background(), user.Id, "recovered-admin", replacementAccountPassword); err == nil {
		t.Fatal("failed factor removal reported successful recovery")
	}
	var stored model.User
	if err := db.First(&stored, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Username != user.Username || stored.Password != user.Password || stored.CredentialGeneration != user.CredentialGeneration || stored.MFAGeneration != user.MFAGeneration {
		t.Fatal("rolled back recovery changed administrator credentials")
	}
	for _, table := range []any{&model.AdminMFAFactor{}, &model.AdminRecoveryCode{}, &model.StepUpGrant{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("rollback lost factor, recovery code or one-use grant")
		}
	}
	db.Callback().Delete().Remove(callback)
	result, err := owner.CompleteRecoveryTransition(context.Background(), user.Id, "recovered-admin", replacementAccountPassword)
	if err != nil {
		t.Fatal(err)
	}
	if result.CredentialGeneration != user.CredentialGeneration+1 || result.MFAGeneration != user.MFAGeneration+1 {
		t.Fatal("recovery failed to invalidate both security generations")
	}
	for _, table := range []any{&model.AdminMFAFactor{}, &model.AdminRecoveryCode{}, &model.StepUpGrant{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("committed recovery left obsolete security authority")
		}
	}
}

func TestAdministratorCredentialChangePreservesFactorAndRetiresStepUpGrants(t *testing.T) {
	db, owner, user := initAccountCommandsDB(t)
	seedAccountSecurity(t, db, user)
	result, err := owner.ChangeCredentialAfterStepUp(context.Background(), user.Id, "updated-admin", replacementAccountPassword)
	if err != nil {
		t.Fatal(err)
	}
	if result.CredentialGeneration != user.CredentialGeneration+1 || result.MFAGeneration != user.MFAGeneration {
		t.Fatal("credential change altered the wrong security generation")
	}
	var factor model.AdminMFAFactor
	if err := db.Where("user_id=?", user.Id).First(&factor).Error; err != nil {
		t.Fatal(err)
	}
	if factor.ActiveSecretCiphertext != "opaque-factor" {
		t.Fatal("credential change altered the enrolled factor")
	}
	var grants int64
	if err := db.Model(&model.StepUpGrant{}).Where("user_id=?", user.Id).Count(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if grants != 0 {
		t.Fatal("credential change retained obsolete step-up authority")
	}
	resultAuth, err := owner.Authenticate(context.Background(), "updated-admin", replacementAccountPassword, "")
	if err != nil || resultAuth.AuthState != panelsecurity.AuthStateMFAPending {
		t.Fatalf("active factor was not required at next login: state=%q err=%v", resultAuth.AuthState, err)
	}
}

type tokenSalt []byte

func (s tokenSalt) InstallSalt() ([]byte, error) { return s, nil }

func TestAPITokenOwnerUsesCurrentScopeAccountAndDurableRevocation(t *testing.T) {
	db, _, user := initAccountCommandsDB(t)
	owner := tokens.Service{Settings: tokenSalt("portable-fixture-install-salt")}
	unregister := tokens.RegisterAPITokenScopeProvider(func() []string { return []string{"portable-component"} })
	t.Cleanup(unregister)
	plaintext, err := owner.AddToken(user.Username, 0, "portable component", "portable-component")
	if err != nil {
		t.Fatal(err)
	}
	var stored model.Tokens
	if err := db.Where("user_id=?", user.Id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Token != "" || stored.TokenHash == "" || strings.Contains(stored.TokenHash, plaintext) {
		t.Fatal("token persistence exposed the reusable credential")
	}
	authorize := func() error {
		_, err := owner.AuthorizeAPIToken(context.Background(), stored.Id, stored.TokenHash, time.Now().Unix())
		return err
	}
	if err := authorize(); err != nil {
		t.Fatal(err)
	}
	unregister()
	if err := authorize(); err == nil {
		t.Fatal("cached token authorized an unregistered component scope")
	}
	unregister = tokens.RegisterAPITokenScopeProvider(func() []string { return []string{"portable-component"} })
	t.Cleanup(unregister)
	if err := db.Model(&model.User{}).Where("id=?", user.Id).Update("force_password_reset", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := authorize(); err == nil {
		t.Fatal("cached token bypassed current account reset requirement")
	}
	if err := db.Model(&model.User{}).Where("id=?", user.Id).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	listed, err := owner.GetUserTokens(user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if len(*listed) != 1 || (*listed)[0].TokenHash != "" || (*listed)[0].Token == plaintext {
		t.Fatal("token listing exposed reusable credentials")
	}
	id := strconv.FormatUint(uint64(stored.Id), 10)
	if err := owner.DeleteToken("foreign-admin", id); !errors.Is(err, tokens.ErrTokenNotFound) {
		t.Fatal("foreign account deleted token")
	}
	if err := authorize(); err != nil {
		t.Fatal("rejected foreign mutation changed token authority")
	}
	if err := owner.SetTokenEnabled(user.Username, id, false); err != nil {
		t.Fatal(err)
	}
	if err := authorize(); err == nil {
		t.Fatal("disabled token retained authority")
	}
}
