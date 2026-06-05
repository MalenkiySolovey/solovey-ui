package importxui

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/deposist/s-ui-x/config"
	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/model"
	"github.com/deposist/s-ui-x/logger"
	"golang.org/x/crypto/hkdf"
	"gorm.io/gorm"
)

const profileKeySalt = "xui-sync-profile-v1"

// profileSecretWarning ensures the "no out-of-database secret configured"
// guidance is logged at most once per process.
var profileSecretWarning sync.Once

type SyncProfileSource struct {
	Type               string `json:"type"`
	URL                string `json:"url,omitempty"`
	Host               string `json:"host,omitempty"`
	Port               int    `json:"port,omitempty"`
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	KeyPath            string `json:"keyPath,omitempty"`
	RemotePath         string `json:"remotePath,omitempty"`
	BaseURL            string `json:"baseUrl,omitempty"`
	ConfirmHostKey     bool   `json:"confirmHostKey,omitempty"`
	HostKeyFingerprint string `json:"hostKeyFingerprint,omitempty"`
	// RestrictPrivate is forced server-side from the saver's auth context (true
	// for an untrusted scoped token) and persists inside the encrypted source so
	// the trusted cron runner re-applies the SSRF/locality block-list to a profile
	// an untrusted token authored. It is never accepted from client input.
	RestrictPrivate bool `json:"restrictPrivate,omitempty"`
}

type SyncProfileInput struct {
	Name            string            `json:"name"`
	SourceType      string            `json:"sourceType"`
	Source          SyncProfileSource `json:"source"`
	Strategy        Strategy          `json:"strategy"`
	OnlyNew         bool              `json:"onlyNew"`
	OnlyNewProvided bool              `json:"-"`
	IncludeSettings bool              `json:"includeSettings"`
	IncludeHistory  bool              `json:"includeHistory"`
	IncludeRouting  bool              `json:"includeRouting"`
	AdminMode       string            `json:"adminMode"`
	Enabled         bool              `json:"enabled"`
	EnabledProvided bool              `json:"-"`
	Schedule        string            `json:"schedule"`
}

func (input *SyncProfileInput) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name            string            `json:"name"`
		SourceType      string            `json:"sourceType"`
		Source          SyncProfileSource `json:"source"`
		Strategy        Strategy          `json:"strategy"`
		OnlyNew         *bool             `json:"onlyNew"`
		IncludeSettings bool              `json:"includeSettings"`
		IncludeHistory  bool              `json:"includeHistory"`
		IncludeRouting  bool              `json:"includeRouting"`
		AdminMode       string            `json:"adminMode"`
		Enabled         *bool             `json:"enabled"`
		Schedule        string            `json:"schedule"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	input.Name = raw.Name
	input.SourceType = raw.SourceType
	input.Source = raw.Source
	input.Strategy = raw.Strategy
	input.IncludeSettings = raw.IncludeSettings
	input.IncludeHistory = raw.IncludeHistory
	input.IncludeRouting = raw.IncludeRouting
	input.AdminMode = raw.AdminMode
	input.Schedule = raw.Schedule
	if raw.OnlyNew != nil {
		input.OnlyNew = *raw.OnlyNew
		input.OnlyNewProvided = true
	}
	if raw.Enabled != nil {
		input.Enabled = *raw.Enabled
		input.EnabledProvided = true
	}
	return nil
}

func SaveSyncProfile(input SyncProfileInput) (*model.XUISyncProfile, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("missing profile name")
	}
	if input.SourceType == "" {
		input.SourceType = input.Source.Type
	}
	if input.Strategy == "" {
		input.Strategy = StrategyMerge
	}
	if err := input.Strategy.Validate(); err != nil {
		return nil, err
	}
	adminMode := AdminMode(input.AdminMode)
	if adminMode == "" {
		adminMode = AdminModeSkip
	}
	if err := adminMode.Validate(); err != nil {
		return nil, err
	}
	// #nosec G117 -- serializes the import source config for encrypted storage; the password field is needed to reconnect to the remote panel and is never logged.
	raw, err := json.Marshal(input.Source)
	if err != nil {
		return nil, err
	}
	ciphertext, salt, err := EncryptProfileSource(raw)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	onlyNew := true
	if input.OnlyNew || input.OnlyNewProvided {
		onlyNew = input.OnlyNew
	}
	enabled := true
	if input.Enabled || input.EnabledProvided {
		enabled = input.Enabled
	}
	profile := &model.XUISyncProfile{
		Name:            input.Name,
		SourceType:      input.SourceType,
		SourceJSON:      ciphertext,
		SourceSalt:      salt,
		Strategy:        string(input.Strategy),
		OnlyNew:         onlyNew,
		IncludeSettings: input.IncludeSettings,
		IncludeHistory:  input.IncludeHistory,
		IncludeRouting:  input.IncludeRouting,
		AdminMode:       string(adminMode),
		Enabled:         enabled,
		Schedule:        input.Schedule,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if profile.Schedule == "" {
		profile.Schedule = "0 */6 * * *"
	}
	if db := database.GetDB(); db != nil {
		var existing model.XUISyncProfile
		err := db.Where("name = ?", profile.Name).First(&existing).Error
		if err == nil {
			profile.Id = existing.Id
			profile.CreatedAt = existing.CreatedAt
			return profile, db.Model(&existing).Updates(syncProfileConfigValues(profile)).Error
		}
		if !database.IsNotFound(err) {
			return nil, err
		}
		if err := db.Model(&model.XUISyncProfile{}).Create(syncProfileConfigValues(profile)).Error; err != nil {
			return nil, err
		}
		if err := db.Where("name = ?", profile.Name).First(profile).Error; err != nil {
			return nil, err
		}
		return profile, nil
	}
	return nil, fmt.Errorf("destination database is not initialized")
}

func syncProfileConfigValues(profile *model.XUISyncProfile) map[string]any {
	return map[string]any{
		"name":             profile.Name,
		"source_type":      profile.SourceType,
		"source_json":      profile.SourceJSON,
		"source_salt":      profile.SourceSalt,
		"strategy":         profile.Strategy,
		"only_new":         profile.OnlyNew,
		"include_settings": profile.IncludeSettings,
		"include_history":  profile.IncludeHistory,
		"include_routing":  profile.IncludeRouting,
		"admin_mode":       profile.AdminMode,
		"enabled":          profile.Enabled,
		"schedule":         profile.Schedule,
		"created_at":       profile.CreatedAt,
		"updated_at":       profile.UpdatedAt,
	}
}

func LoadSyncProfileSource(profile model.XUISyncProfile) (SyncProfileSource, error) {
	raw, err := DecryptProfileSource(profile.SourceJSON, profile.SourceSalt)
	if err != nil {
		return SyncProfileSource{}, err
	}
	var source SyncProfileSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return SyncProfileSource{}, err
	}
	return source, nil
}

func EncryptProfileSource(plaintext []byte) ([]byte, []byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}
	keys, err := profileEncryptionKeys(salt)
	if err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(keys[0])
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	ciphertext := append([]byte{}, nonce...)
	ciphertext = gcm.Seal(ciphertext, nonce, plaintext, nil)
	return ciphertext, salt, nil
}

func DecryptProfileSource(ciphertext []byte, salt []byte) ([]byte, error) {
	keys, err := profileEncryptionKeys(salt)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, key := range keys {
		plaintext, err := decryptWithProfileKey(ciphertext, key)
		if err == nil {
			return plaintext, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("profile ciphertext could not be decrypted")
	}
	return nil, lastErr
}

func decryptWithProfileKey(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("profile ciphertext too short")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	payload := ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, payload, nil)
}

// profileEncryptionKeys returns the ordered AES-256 key candidates for rowSalt.
// keys[0] is used for encryption; DecryptProfileSource tries every candidate so
// a profile sealed under an older seed still opens (and is re-sealed under the
// preferred seed on its next save).
func profileEncryptionKeys(rowSalt []byte) ([][]byte, error) {
	if keyFile := strings.TrimSpace(os.Getenv("XUI_PROFILE_KEY_FILE")); keyFile != "" {
		// #nosec G304 G703 -- keyFile path comes from the operator-set XUI_PROFILE_KEY_FILE env var.
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, err
		}
		key := bytesOrBase64Key(raw)
		if len(key) != 32 {
			return nil, fmt.Errorf("XUI_PROFILE_KEY_FILE must contain 32 bytes raw or base64")
		}
		return [][]byte{key}, nil
	}
	if strings.TrimSpace(os.Getenv("SUI_SECRET")) == "" {
		profileSecretWarning.Do(func() {
			logger.Warning("xui-sync: neither SUI_SECRET nor XUI_PROFILE_KEY_FILE is set; remote-panel credentials are encrypted with a key derived from the random settings.secret stored in the same database (falling back to a predictable default only if it is unavailable). Set SUI_SECRET or XUI_PROFILE_KEY_FILE so the credentials can be recovered only with an out-of-database secret.")
		})
	}
	salt := append([]byte(profileKeySalt+":"), rowSalt...)
	seeds := profileSeeds()
	keys := make([][]byte, 0, len(seeds))
	for _, seed := range seeds {
		reader := hkdf.New(sha256.New, seed, salt, nil)
		key := make([]byte, 32)
		if _, err := io.ReadFull(reader, key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no profile encryption seed available")
	}
	return keys, nil
}

// profileSeeds returns the HKDF seed material for profile encryption, preferred
// first: an explicit SUI_SECRET (out-of-database), then the random per-install
// settings.secret, then the legacy config.GetSecret() value (predictable
// name:path). EncryptProfileSource uses the first; DecryptProfileSource tries
// them ALL, so every seed that could have sealed a profile stays a decrypt
// candidate — e.g. a profile sealed under settings.secret still opens after the
// operator later sets SUI_SECRET (the very transition the at-rest warning
// recommends). The candidates are independent (not else-chained) for exactly
// this reason.
func profileSeeds() [][]byte {
	var seeds [][]byte
	if env := strings.TrimSpace(os.Getenv("SUI_SECRET")); env != "" {
		seeds = appendSeed(seeds, []byte(env))
	}
	if secret, ok := settingsSecret(); ok {
		seeds = appendSeed(seeds, secret)
	}
	seeds = appendSeed(seeds, []byte(config.GetSecret()))
	return seeds
}

// settingsSecret reads the random per-install secret (settings.secret) straight
// from the settings table. It is stored in plaintext and auto-initialized by the
// panel at startup; importxui reads it directly to avoid depending on the
// service layer. Returns ok=false before the database or secret exist.
func settingsSecret() ([]byte, bool) {
	db := database.GetDB()
	if db == nil {
		return nil, false
	}
	var setting model.Setting
	if err := db.Where("key = ?", "secret").First(&setting).Error; err != nil {
		return nil, false
	}
	if strings.TrimSpace(setting.Value) == "" {
		return nil, false
	}
	return []byte(setting.Value), true
}

func appendSeed(seeds [][]byte, seed []byte) [][]byte {
	if len(seed) == 0 {
		return seeds
	}
	for _, existing := range seeds {
		if bytes.Equal(existing, seed) {
			return seeds
		}
	}
	return append(seeds, seed)
}

func bytesOrBase64Key(raw []byte) []byte {
	trimmed := []byte(strings.TrimSpace(string(raw)))
	if len(trimmed) == 32 {
		return trimmed
	}
	decoded, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err == nil {
		return decoded
	}
	return raw
}

func UpdateSyncProfileRun(tx *gorm.DB, profile *model.XUISyncProfile, status string, summary any, at int64) error {
	if at == 0 {
		at = time.Now().Unix()
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return tx.Model(profile).Updates(map[string]any{
		"last_run_at":      at,
		"last_run_status":  status,
		"last_run_summary": raw,
		"updated_at":       at,
	}).Error
}
