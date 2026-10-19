package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const referenceFakeA = "123456:deterministic-token-alpha"
const referenceFakeB = "123456:deterministic-token-bravo"

func secretAccessFixture(t *testing.T) (*SettingService, secretref.Access) {
	t.Helper()
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	s := initSettingTestDB(t)
	if _, err := s.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := s.setEncryptedString(testFixturePrimaryBotTokenKey, referenceFakeA); err != nil {
		t.Fatal(err)
	}
	return s, secretref.Access{Reference: secretref.NewReference("test.fixture-primary", testFixturePrimaryBotTokenKey),
		Actor: secretref.Actor{Component: "test.fixture-primary"}, Operation: secretref.UseForOwner, IntentID: "fixture-secret-use"}
}

func TestSecretReferenceOwnerScopeMaskingAndAudit(t *testing.T) {
	s, access := secretAccessFixture(t)
	for _, mutate := range []func(*secretref.Access){
		func(a *secretref.Access) { a.Actor.Component = "test.fixture-secondary" },
		func(a *secretref.Access) {
			a.Reference.Owner, a.Actor.Component = "test.fixture-secondary", "test.fixture-secondary"
		},
		func(a *secretref.Access) { a.Reference.ID = testFixturePrimaryEnabledKey },
		func(a *secretref.Access) { a.Operation = "REVEAL" },
	} {
		candidate := access
		mutate(&candidate)
		if _, err := s.AccessComponentSecret(t.Context(), candidate, func([]byte) error { t.Error("denied callback ran"); return nil }); err != secretref.ErrDenied {
			t.Fatalf("owner boundary failed: %v", err)
		}
	}
	presence := access
	presence.Operation = secretref.Presence
	view, err := s.AccessComponentSecret(t.Context(), presence, nil)
	if err != nil || !view.Present || len(view.RotationRevision) != 64 {
		t.Fatalf("presence failed: %v", err)
	}
	var borrowed []byte
	view, err = s.AccessComponentSecret(t.Context(), access, func(value []byte) error {
		if string(value) != referenceFakeA {
			t.Error("owner received wrong secret")
		}
		borrowed = value
		return nil
	})
	if err != nil || !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("borrowed bytes not wiped")
	}
	if _, err := s.AccessComponentSecret(t.Context(), access, func([]byte) error { return errors.New(referenceFakeA) }); err != secretref.ErrUseFailed {
		t.Fatal("callback error text escaped")
	}
	settings, err := s.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	var audits []model.AuditEvent
	if err := s.settingDatabase().Where("event = ?", "component_secret_use").Find(&audits).Error; err != nil || len(audits) != 2 {
		t.Fatalf("audit count=%d err=%v", len(audits), err)
	}
	for _, value := range []any{view, settings, audits} {
		encoded, _ := json.Marshal(value)
		if strings.Contains(string(encoded), referenceFakeA) {
			t.Fatal("plaintext in masked projection/audit")
		}
	}
	var details struct {
		Correlation struct {
			CorrelationID string `json:"correlationId"`
		} `json:"correlation"`
	}
	if err := json.Unmarshal(audits[0].Details, &details); err != nil || details.Correlation.CorrelationID != view.Correlation.CorrelationID {
		t.Fatal("secret audit lost correlation")
	}
}

func TestSecretReferenceConcurrentReplacementUsesCapturedSnapshot(t *testing.T) {
	s, access := secretAccessFixture(t)
	entered, release, completed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var oldView secretref.View
	go func() {
		var err error
		oldView, err = s.AccessComponentSecret(t.Context(), access, func(value []byte) error {
			if string(value) != referenceFakeA {
				return errors.New("wrong snapshot")
			}
			close(entered)
			<-release
			if string(value) != referenceFakeA {
				return errors.New("snapshot changed")
			}
			return nil
		})
		completed <- err
	}()
	select {
	case <-entered:
	case err := <-completed:
		t.Fatalf("use did not enter: %v", err)
	}
	if err := s.setEncryptedString(testFixturePrimaryBotTokenKey, referenceFakeB); err != nil {
		close(release)
		<-completed
		t.Fatal(err)
	}
	newView, err := s.AccessComponentSecret(t.Context(), access, func(value []byte) error {
		if string(value) != referenceFakeB {
			return errors.New("replacement not observed")
		}
		return nil
	})
	close(release)
	if oldErr := <-completed; oldErr != nil || err != nil || oldView.RotationRevision == newView.RotationRevision {
		t.Fatal("replacement mixed owner snapshots/revisions")
	}
}

func TestSecretReferenceLegacyWrongKeyAADCancellationAndAuditFailure(t *testing.T) {
	s, access := secretAccessFixture(t)
	setStored := func(value string) {
		t.Helper()
		if err := s.settingDatabase().Model(&model.Setting{}).Where("key = ?", access.Reference.ID).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
	setStored(referenceFakeA)
	view, err := s.AccessComponentSecret(t.Context(), access, func(value []byte) error {
		if string(value) != referenceFakeA {
			t.Error("legacy storage changed")
		}
		return nil
	})
	if err != nil || view.RotationRevision != "" {
		t.Fatal("legacy plaintext compatibility changed")
	}
	box, err := s.settingsSecretCodec().Secretbox()
	if err != nil {
		t.Fatal(err)
	}
	wrongAAD, err := box.EncryptString(referenceFakeA, "wrong-owner-aad")
	if err != nil {
		t.Fatal(err)
	}
	setStored(wrongAAD)
	denyUse := func([]byte) error { t.Error("failed access exposed plaintext"); return nil }
	if _, err := s.AccessComponentSecret(t.Context(), access, denyUse); err != secretref.ErrUnavailable {
		t.Fatal("wrong AAD accepted")
	}
	if err := s.setEncryptedString(access.Reference.ID, referenceFakeA); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_SECRETBOX_KEY", base64.RawURLEncoding.EncodeToString([]byte("abcdef0123456789abcdef0123456789")))
	if _, err := s.AccessComponentSecret(t.Context(), access, denyUse); err != secretref.ErrUnavailable {
		t.Fatal("wrong key accepted")
	}
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.AccessComponentSecret(ctx, access, denyUse); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled access accepted")
	}
	db := s.settingDatabase()
	const hook = "r3_reject_secret_audit"
	if err := db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("fixture-audit-write-failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(hook) })
	if _, err := s.AccessComponentSecret(t.Context(), access, denyUse); err != secretref.ErrUnavailable {
		t.Fatal("failed audit allowed plaintext use")
	}
}
