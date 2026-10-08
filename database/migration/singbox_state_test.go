package migration

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/database/migration/steps"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbschema "github.com/MalenkiySolovey/solovey-ui/database/schema"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/assembly"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// This database is a transaction fault fixture assembled from reviewed owner
// shapes. Exact stable-source DB/archive replay is recorded separately; this
// helper does not claim to capture a historical database with current code.
func singBoxMigrationFixture(t *testing.T) (string, *gorm.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migration.db")
	db, err := gorm.Open(sqlite.Open(path+"?_foreign_keys=on"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Setting{}, &model.Tls{}, &model.Inbound{}, &model.Outbound{}, &model.Service{}, &model.Endpoint{}, &model.Client{}, &model.Stats{}, &model.Tokens{}); err != nil {
		t.Fatal(err)
	}
	settings := []model.Setting{{Key: "version", Value: configidentity.GetVersion()}, {Key: "coreSchemaVersion", Value: "1.11"}, {Key: "config", Value: `{"dns":{"servers":[{"tag":"old","address":"9.9.9.9"}],"final":"old","independent_cache":false},"route":{"rule_set":[{"type":"remote","tag":"operator-set","format":"binary","url":"https://fixture.invalid/operator.srs","download_detour":"direct","update_interval":"3h"}]},"experimental":{"cache_file":{"enabled":false,"store_rdrc":true}}}`}}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatal(err)
	}
	profile := model.Tls{Name: "unissued", Server: []byte(`{"enabled":false,"acme":{"domain":["fixture.invalid"],"email":"fixture@invalid.example"}}`), Client: []byte(`{}`)}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Outbound{{Type: "direct", Tag: "direct", Options: []byte(`{}`)}, {Type: "hysteria2", Tag: "old-h2", Options: []byte(`{"server":"127.0.0.1","server_port":443,"tls":{"enabled":true,"insecure":true}}`)}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Stats{DateTime: 1, Tag: "fixture-counter", Resource: "fixture", Direction: true, Traffic: 71}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{Id: 1, Username: "inert-fixture", ForcePasswordReset: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Tokens{Desc: "inert-fixture", Token: "fixture-hash-only", UserId: 1, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureOperationsMigrationJournal(db); err != nil {
		t.Fatal(err)
	}
	if err := recordOperationsMigrationState(db, "APPLIED", ""); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func semanticPreimage(t *testing.T, db *gorm.DB, omitJournal bool) [32]byte {
	t.Helper()
	var tables []string
	if err := db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	var image bytes.Buffer
	for _, table := range tables {
		if omitJournal && table == "migration_journal_v1" {
			continue
		}
		if strings.ContainsAny(table, `"\`) {
			t.Fatal("unexpected test table identity")
		}
		var schema string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&schema).Error; err != nil {
			t.Fatal(err)
		}
		image.WriteString(schema)
		rows, err := db.Raw(fmt.Sprintf(`SELECT * FROM "%s" ORDER BY rowid`, table)).Rows()
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			image.Write(encoded)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
	return sha256.Sum256(image.Bytes())
}

func TestSingBoxMigrationOneDurableBoundaryAndExactNoop(t *testing.T) {
	path, db := singBoxMigrationFixture(t)
	var statsBefore, tokensBefore string
	if err := db.Raw("SELECT traffic FROM stats").Scan(&statsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT token FROM tokens").Scan(&tokensBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigratePath(path, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := validateCurrentMigrationJournals(db); err != nil {
		t.Fatal(err)
	}
	version, err := readVersionSetting(db, "coreSchemaVersion")
	if err != nil || version != dbschema.CurrentCoreVersion {
		t.Fatal("complete schema boundary was not published")
	}
	var providers []model.TLSCertificateProvider
	if err := db.Find(&providers).Error; err != nil || len(providers) != 1 || !secretbox.IsEncrypted(providers[0].OptionsEnvelope) {
		t.Fatal("provider canonical envelope was not published")
	}
	definitions, err := entitytls.ReadProviderDefinitions(db)
	if err != nil || len(definitions) != 1 {
		t.Fatal("published provider cannot be opened by its database owner")
	}
	var profile model.Tls
	if err := db.Where("id>0").First(&profile).Error; err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(profile.Server, []byte(`"acme"`)) || !bytes.Contains(profile.Server, []byte(`"certificate_provider"`)) {
		t.Fatal("provider profile was only partially converted")
	}
	var h2 model.Outbound
	if err := db.Where("tag=?", "old-h2").First(&h2).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(h2.Options, []byte(`"disable_chrome_parrot":true`)) {
		t.Fatal("old Hysteria2 behavior changed")
	}
	projection, err := assembly.BuildCandidateProjectionFromDB(db, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := singboxvalidation.ValidateConfig(projection.Config); err != nil {
		t.Fatal(err)
	}
	var afterStats, afterTokens string
	_ = db.Raw("SELECT traffic FROM stats").Scan(&afterStats).Error
	_ = db.Raw("SELECT token FROM tokens").Scan(&afterTokens).Error
	if afterStats != statsBefore || afterTokens != tokensBefore {
		t.Fatal("migration changed lifetime counters or token state")
	}
	once := semanticPreimage(t, db, false)
	if err := MigratePath(path, Options{}); err != nil {
		t.Fatal(err)
	}
	if semanticPreimage(t, db, false) != once {
		t.Fatal("migration once differs from twice, including envelopes and journal")
	}
	// Current absence is a current choice after the successful cutoff.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(h2.Options, &fields)
	delete(fields, "disable_chrome_parrot")
	current, _ := json.Marshal(fields)
	if err := db.Model(&model.Outbound{}).Where("id=?", h2.Id).Update("options", json.RawMessage(current)).Error; err != nil {
		t.Fatal(err)
	}
	projection, err = assembly.BuildCandidateProjectionFromDB(db, "", true)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Outbounds []map[string]json.RawMessage `json:"outbounds"`
	}
	_ = json.Unmarshal(projection.Config, &root)
	for _, row := range root.Outbounds {
		var tag string
		_ = json.Unmarshal(row["tag"], &tag)
		if tag == "old-h2" {
			if _, present := row["disable_chrome_parrot"]; present {
				t.Fatal("post-boundary current absence was treated as historical")
			}
		}
	}
}

func TestSingBoxMigrationDoesNotOpenRuntimeCache(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("lateFailure=%t", fail), func(t *testing.T) {
			path, db := singBoxMigrationFixture(t)
			cachePath := filepath.Join(filepath.Dir(path), "runtime-cache.db")
			// A controlled corrupt cache would be reset if runtime initialization
			// opened it. This is a lifecycle fault fixture, not an old capture.
			preimage := make([]byte, 8192)
			copy(preimage, []byte("controlled-cache-preimage"))
			if err := os.WriteFile(cachePath, preimage, 0600); err != nil {
				t.Fatal(err)
			}
			var setting model.Setting
			if err := db.Where("key=?", "config").First(&setting).Error; err != nil {
				t.Fatal(err)
			}
			var root map[string]json.RawMessage
			if json.Unmarshal([]byte(setting.Value), &root) != nil {
				t.Fatal("fixture config invalid")
			}
			root["experimental"], _ = json.Marshal(map[string]any{"cache_file": map[string]any{"enabled": true, "path": cachePath, "store_rdrc": true}})
			data, _ := json.Marshal(root)
			if err := db.Model(&model.Setting{}).Where("key=?", "config").Update("value", string(data)).Error; err != nil {
				t.Fatal(err)
			}
			if fail {
				if err := db.Exec(`CREATE TRIGGER reject_cache_wave_version BEFORE UPDATE ON settings WHEN NEW.key='coreSchemaVersion' AND NEW.value='1.12' BEGIN SELECT RAISE(ABORT,'injected version publication'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			err := MigratePath(path, Options{})
			if (err != nil) != fail {
				t.Fatal("unexpected migration result")
			}
			after, err := os.ReadFile(cachePath)
			if err != nil || !bytes.Equal(after, preimage) {
				t.Fatal("offline migration opened or changed runtime cache")
			}
		})
	}
}

func TestSingBoxMigrationLateFailuresPreservePreimageAndRetry(t *testing.T) {
	for _, fault := range []string{"complete_validation", "version_publication", "journal_publication"} {
		t.Run(fault, func(t *testing.T) {
			path, db := singBoxMigrationFixture(t)
			options := Options{}
			if fault == "complete_validation" {
				options.ProjectRuntimeFiles = func(*gorm.DB, []byte) ([]byte, error) { return nil, errors.New("injected complete candidate failure") }
			}
			if fault == "version_publication" {
				if err := db.Exec(`CREATE TRIGGER reject_wave_version BEFORE UPDATE ON settings WHEN NEW.key='coreSchemaVersion' AND NEW.value='1.12' BEGIN SELECT RAISE(ABORT,'injected version publication'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			if fault == "journal_publication" {
				if err := db.Exec(`CREATE TRIGGER reject_wave_journal BEFORE UPDATE ON migration_journal_v1 WHEN NEW.step_id='core-1.12-singbox-stored-state' AND NEW.state='APPLIED' BEGIN SELECT RAISE(ABORT,'injected journal publication'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			before := semanticPreimage(t, db, true)
			if err := MigratePath(path, options); err == nil {
				t.Fatal("late failure was ignored")
			}
			if semanticPreimage(t, db, true) != before {
				t.Fatal("failed migration changed the source schema or durable semantic rows")
			}
			var state string
			if err := db.Raw("SELECT state FROM migration_journal_v1 WHERE step_id=?", steps.SingBoxStateStepID).Scan(&state).Error; err != nil || state != "FAILED" {
				t.Fatal("rollback state was not recorded")
			}
			_ = db.Exec("DROP TRIGGER IF EXISTS reject_wave_version").Error
			_ = db.Exec("DROP TRIGGER IF EXISTS reject_wave_journal").Error
			if err := MigratePath(path, Options{}); err != nil {
				t.Fatal(err)
			}
			var retry uint
			_ = db.Raw("SELECT retry_count FROM migration_journal_v1 WHERE step_id=?", steps.SingBoxStateStepID).Scan(&retry).Error
			if retry != 1 {
				t.Fatal("corrected migration did not resume the same journal step")
			}
		})
	}
}

func TestSingBoxMigrationManualPreflightIsReadOnlyAndActionable(t *testing.T) {
	path, db := singBoxMigrationFixture(t)
	source := `{"experimental":{"clash_api":{"cache_file":"operator-cache.db","store_fakeip":true}}}`
	if err := db.Model(&model.Setting{}).Where("key=?", "config").Update("value", source).Error; err != nil {
		t.Fatal(err)
	}
	before := semanticPreimage(t, db, false)
	err := MigratePath(path, Options{})
	var rejection *diagnostics.Rejection
	if !errors.As(err, &rejection) || rejection.Finding.MigrationOutcome != diagnostics.ManualRequired || rejection.Finding.Path == "" {
		t.Fatal("cache policy conflict did not return an actionable owner diagnostic")
	}
	if semanticPreimage(t, db, false) != before {
		t.Fatal("manual preflight changed source state or journal")
	}
	if err := db.Model(&model.Setting{}).Where("key=?", "config").Update("value", `{}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigratePath(path, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestSingBoxMigrationJournalChecksumDriftFailsClosed(t *testing.T) {
	path, db := singBoxMigrationFixture(t)
	contract := coreJournalContracts()[1]
	if err := ensureCoreMigrationJournal(db, contract); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE migration_journal_v1 SET checksum=?,state='FAILED' WHERE step_id=?", strings.Repeat("0", 64), contract.ID).Error; err != nil {
		t.Fatal(err)
	}
	before := semanticPreimage(t, db, true)
	if err := MigratePath(path, Options{}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal("changed migration contract was admitted")
	}
	if semanticPreimage(t, db, true) != before {
		t.Fatal("checksum drift changed semantic source rows")
	}
	var states []string
	_ = db.Raw("SELECT state FROM migration_journal_v1 WHERE step_id=?", contract.ID).Scan(&states).Error
	sort.Strings(states)
	if len(states) != 1 || states[0] != "RECOVERY_REQUIRED" {
		t.Fatal("checksum drift did not retain recovery authority")
	}
}

func TestSingBoxMigrationSubscriptionManualPreservesPreimageAndRetry(t *testing.T) {
	path, db := singBoxMigrationFixture(t)
	row := model.Setting{Key: "subJsonExt", Value: `{"experimental":{"clash_api":{"store_fakeip":true}},"custom":{"false":false,"empty":""}}`}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	before := semanticPreimage(t, db, true)
	err := MigratePath(path, Options{})
	var rejection *diagnostics.Rejection
	if !errors.As(err, &rejection) || !strings.HasPrefix(rejection.Finding.Path, "subJsonExt.") || rejection.Finding.Code != "CLASH_CACHE_EXPLICIT_CHOICE_REQUIRED" {
		t.Fatal("subscription did not share fixed core-owner diagnostic")
	}
	if before != semanticPreimage(t, db, true) {
		t.Fatal("manual subscription candidate changed staged semantic rows or encryption lifecycle")
	}
	if err := db.Model(&model.Setting{}).Where("key = ?", row.Key).Update("value", `{"custom":{"false":false,"empty":""},"inbounds":[{"type":"mixed","tag":"fixture","sniff":false}]}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigratePath(path, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("key = ?", row.Key).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.Value, "sniff") || !strings.Contains(row.Value, `"false":false`) || !strings.Contains(row.Value, `"empty":""`) {
		t.Fatal("subscription automatic correction erased custom false/empty state")
	}
}

func TestSingBoxMigrationSQLiteCommitFailureRollsBackAndAllowsCorrection(t *testing.T) {
	path, db := singBoxMigrationFixture(t)
	for _, statement := range []string{
		`CREATE TABLE fault_parent(id INTEGER PRIMARY KEY)`,
		`CREATE TABLE fault_child(parent_id INTEGER REFERENCES fault_parent(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER fail_deferred_commit AFTER UPDATE ON settings WHEN NEW.key='coreSchemaVersion' AND NEW.value='1.12' BEGIN INSERT INTO fault_child(parent_id) VALUES(999); END`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	before := semanticPreimage(t, db, true)
	err := MigratePath(path, Options{})
	if err == nil || !strings.Contains(err.Error(), "commit migration") {
		t.Fatal("deferred foreign-key failure did not reach SQLite commit")
	}
	if semanticPreimage(t, db, true) != before {
		t.Fatal("failed SQLite commit changed the source preimage")
	}
	var state, code string
	if err := db.Raw("SELECT state,error_code FROM migration_journal_v1 WHERE step_id=?", steps.SingBoxStateStepID).Row().Scan(&state, &code); err != nil {
		t.Fatal(err)
	}
	if state != "FAILED" || code != "core_migration_commit_rolled_back" {
		t.Fatal("independently proven SQLite rollback was not retry-safe")
	}
	if err := db.Exec("DROP TRIGGER fail_deferred_commit").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigratePath(path, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := validateCurrentMigrationJournals(db); err != nil {
		t.Fatal(err)
	}
}
