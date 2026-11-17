package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	passwordutil "github.com/MalenkiySolovey/solovey-ui/backend/security/password"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func bootstrapFixture(t *testing.T) (*gorm.DB, Initializer, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "panel.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_settings_key ON settings(key)").Error; err != nil {
		t.Fatal(err)
	}
	return db, Initializer{DataDirectory: root, DatabasePath: path, ApplicationVersion: "1.2", CoreSchemaVersion: "1.13", CompareVersions: func(left, right string) (int, bool) { return strings.Compare(left, right), left != "malformed" }}, path
}

func TestBootstrapAdministratorFileAndPolicyFollowCommittedAccount(t *testing.T) {
	for _, reject := range []bool{false, true} {
		name := "commit"
		if reject {
			name = "transaction-rejected"
		}
		t.Run(name, func(t *testing.T) {
			db, owner, path := bootstrapFixture(t)
			if reject {
				if err := db.Exec("CREATE TRIGGER reject_session_policy BEFORE INSERT ON settings BEGIN SELECT RAISE(FAIL, 'policy rejected'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			err := owner.EnsureInitialAdmin(db, path)
			passwordPath := owner.InitialAdminPasswordPath(path)
			if reject {
				if err == nil {
					t.Fatal("bootstrap transaction unexpectedly committed")
				}
				var count int64
				if err := db.Model(&model.User{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("failed bootstrap left account count=%d err=%v", count, err)
				}
				if _, err := os.Stat(passwordPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed bootstrap published credential file: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(passwordPath)
			if err != nil {
				t.Fatal(err)
			}
			password := strings.TrimSpace(string(data))
			if len(password) != 24 {
				t.Fatal("bootstrap credential has unexpected length")
			}
			var account model.User
			if err := db.First(&account).Error; err != nil {
				t.Fatal(err)
			}
			if ok, _, err := passwordutil.Verify(context.Background(), account.Password, password); err != nil || !ok {
				t.Fatalf("credential verification failed: %v", err)
			}
			if !account.ForcePasswordReset || account.CredentialGeneration != 1 || account.MFAGeneration != 1 {
				t.Fatal("bootstrap account authority differs")
			}
			var policy model.Setting
			if err := db.Where("key = ?", "sessionLifetimePolicy").First(&policy).Error; err != nil {
				t.Fatal(err)
			}
			if policy.Value != "bounded_v1" {
				t.Fatalf("fresh session policy=%q", policy.Value)
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(passwordPath)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("credential permissions=%v err=%v", info, err)
				}
			}
			if err := owner.EnsureInitialAdmin(db, path); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(passwordPath)
			if err != nil || !bytes.Equal(data, after) {
				t.Fatal("existing account credential file was rewritten")
			}
		})
	}
}

func TestBootstrapLegacyPasswordRehashPreservesExistingResetPolicy(t *testing.T) {
	db, owner, _ := bootstrapFixture(t)
	original := model.User{Username: "legacy", Password: "existing-plaintext", CredentialGeneration: 4, ForcePasswordReset: false}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	if err := owner.RehashLegacyPasswords(db); err != nil {
		t.Fatal(err)
	}
	var first model.User
	if err := db.First(&first, original.Id).Error; err != nil {
		t.Fatal(err)
	}
	if first.ForcePasswordReset || first.PasswordPolicyVersion != 0 || first.CredentialGeneration != 5 {
		t.Fatal("legacy non-default reset policy or generation changed")
	}
	if ok, _, err := passwordutil.Verify(context.Background(), first.Password, "existing-plaintext"); err != nil || !ok {
		t.Fatalf("legacy rehash verification failed: %v", err)
	}
	if err := owner.RehashLegacyPasswords(db); err != nil {
		t.Fatal(err)
	}
	var again model.User
	if err := db.First(&again, original.Id).Error; err != nil || again.Password != first.Password || again.CredentialGeneration != first.CredentialGeneration {
		t.Fatal("legacy password adaptation is not idempotent")
	}
}

func TestBootstrapVersionRejectionDoesNotChangeDatabase(t *testing.T) {
	for _, actual := range []string{"9.9", "malformed"} {
		t.Run(actual, func(t *testing.T) {
			db, owner, path := bootstrapFixture(t)
			if err := db.Create(&model.Setting{Key: "version", Value: actual}).Error; err != nil {
				t.Fatal(err)
			}
			raw, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var unsupported *UnsupportedVersionError
			if err := owner.PreflightSupportedVersion(path); !errors.As(err, &unsupported) || unsupported.Actual != actual {
				t.Fatalf("version rejection=%v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("read-only version rejection changed database bytes")
			}
		})
	}
}
