package store

import (
	"bytes"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fixedResealCodec struct{ candidates []settingscrypto.Candidate }

func (codec fixedResealCodec) SecretboxCandidates() ([]settingscrypto.Candidate, error) {
	return codec.candidates, nil
}

func TestResealSecretSettingsRollsBackOnUndecryptableRow(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "configured-for-test")
	db, err := gorm.Open(sqlite.Open("file:settings-reseal-rollback?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	primary, err := secretbox.NewRawKey(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := secretbox.NewRawKey(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	legacyValue, err := legacy.EncryptString("secret", "a-valid")
	if err != nil {
		t.Fatal(err)
	}
	rows := []model.Setting{
		{Key: "a-valid", Value: legacyValue},
		{Key: "z-corrupt", Value: secretbox.Prefix + "not-valid"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	codec := fixedResealCodec{candidates: []settingscrypto.Candidate{
		{Name: "primary", Box: primary},
		{Name: "legacy", Box: legacy},
	}}
	keys := map[string]struct{}{"a-valid": {}, "z-corrupt": {}}
	if count, err := ResealSecretSettings(db, codec, keys); err == nil || count != 0 {
		t.Fatalf("reseal count=%d error=%v, want atomic failure", count, err)
	}
	var stored model.Setting
	if err := db.Where("key = ?", "a-valid").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Value != legacyValue {
		t.Fatal("failed reseal committed an earlier row instead of rolling back")
	}
}

func TestResealSecretSettingsRejectsMissingAuthority(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "configured-for-test")
	if _, err := ResealSecretSettings(nil, fixedResealCodec{}, nil); err == nil {
		t.Fatal("reseal accepted missing settings persistence")
	}
	db, err := gorm.Open(sqlite.Open("file:settings-reseal-authority?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResealSecretSettings(db, fixedResealCodec{}, nil); err == nil {
		t.Fatal("reseal accepted an empty key-candidate set")
	}
}
