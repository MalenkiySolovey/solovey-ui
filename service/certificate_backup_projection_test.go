package service

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	"github.com/MalenkiySolovey/solovey-ui/database/migration/steps"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type certificateArchiveFile struct{ *bytes.Reader }

func (certificateArchiveFile) Close() error { return nil }

// Controlled current fixture for archive/transaction fault qualification.
// Exact stable historical private archives are replayed separately.
func TestCertificatePrivateArchiveWholeMigrationAndRuntimeProjection(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", root)
	t.Setenv(backup.LogicalFileBackupEnv, backup.LogicalFileBackupRequired)
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if err := migration.EnsureCurrentSchemaJournal(dbsqlite.DB(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	cert, key := makeSelfSignedCertPEM(t, time.Now().Add(24*time.Hour))
	paths := []string{filepath.Join(t.TempDir(), "certificate.pem"), filepath.Join(t.TempDir(), "key.pem")}
	for i, data := range [][]byte{cert, key} {
		if err := os.WriteFile(paths[i], data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	server, _ := json.Marshal(map[string]any{"enabled": true, "certificate_path": paths[0], "key_path": paths[1], "client_authentication": "require-and-verify", "client_certificate_path": []string{paths[0]}})
	client, _ := json.Marshal(map[string]any{"enabled": true, "insecure": true, "client_certificate_path": paths[0], "client_key_path": paths[1]})
	profile := model.Tls{Name: "fixture-active-paths", Server: server, Client: client}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Inbound{Type: "trojan", Tag: "fixture-trojan", TlsId: profile.Id, Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":10443}`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Outbound{Type: "trojan", Tag: "fixture-client", Options: json.RawMessage(`{"server":"127.0.0.1","server_port":10443,"password":"inert-fixture","tls":` + string(client) + `}`)}).Error; err != nil {
		t.Fatal(err)
	}
	var baseSetting model.Setting
	if err := db.Where("key = ?", "config").First(&baseSetting).Error; err != nil {
		t.Fatal(err)
	}
	var base map[string]json.RawMessage
	_ = json.Unmarshal([]byte(baseSetting.Value), &base)
	base["http_clients"] = json.RawMessage(`[{"tag":"file-client","engine":"go","tls":` + string(client) + `}]`)
	baseData, _ := json.Marshal(base)
	if err := db.Model(&model.Setting{}).Where("key = ?", "config").Update("value", string(baseData)).Error; err != nil {
		t.Fatal(err)
	}
	provider := entitytls.ProviderDefinition{Tag: "unbound-fixture", Type: "acme", RuntimeMode: entitytls.LegacyInlineMode, Options: json.RawMessage(`{"domain":["fixture.invalid"],"email":"operator@fixture.invalid","external_account":{"key_id":"inert-id","mac_key":"aW5lcnQ="}}`)}
	if err := entitytls.WriteProviderDefinitions(db, []entitytls.ProviderDefinition{provider}); err != nil {
		t.Fatal(err)
	}
	// Rehearse the planned boundary, with source schema recorded by the manifest.
	if err := db.Model(&model.Setting{}).Where("key = ?", "coreSchemaVersion").Update("value", "1.11").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("step_id = ?", steps.SingBoxStateStepID).Delete(&model.MigrationJournal{}).Error; err != nil {
		t.Fatal(err)
	}
	archive, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	probePath := filepath.Join(t.TempDir(), "private-candidate.db")
	if err := os.WriteFile(probePath, archive, 0600); err != nil {
		t.Fatal(err)
	}
	probe, err := gorm.Open(sqlite.Open(probePath), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlProbe, _ := probe.DB()
	defer sqlProbe.Close()
	var ownerFiles []backup.OwnerFile
	if err := probe.Find(&ownerFiles).Error; err != nil {
		t.Fatal(err)
	}
	if err := restoreCertificateFiles(ctx, probe, ownerFiles, false); err != nil {
		t.Fatalf("private owner rebind: %v", err)
	}
	if err := migration.MigratePath(probePath, migration.Options{ProjectRuntimeFiles: func(tx *gorm.DB, config []byte) ([]byte, error) {
		return projectCertificateRuntimeFiles(ctx, tx, ownerFiles, config)
	}}); err != nil {
		t.Fatalf("private owner complete migration: %v", err)
	}
	backup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { backup.SetSendSighupHook(nil) })
	rehearsal, err := backup.Rehearse(ctx, bytes.NewReader(archive))
	if err != nil || !rehearsal.Possible || rehearsal.Manifest.CoreSchema != "1.11" || rehearsal.MigrationPlan != "REHEARSED" {
		t.Fatalf("whole archived owner candidate rejected: error=%v reasons=%v", err, rehearsal.ReasonCodes)
	}
	if _, err := os.Stat(filepath.Join(root, "certs")); !os.IsNotExist(err) {
		t.Fatal("rehearsal published live certificate files")
	}
	// Restore into a separate valid fresh target. The source fixture deliberately
	// lost its files; it cannot serve as the mandatory pre-restore recovery DB.
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	if err := migration.EnsureCurrentSchemaJournal(dbsqlite.DB(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := backup.Restore(certificateArchiveFile{bytes.NewReader(archive)}); err != nil {
		t.Fatal(err)
	}
	current := dbsqlite.DB()
	projection, err := NewSingBoxConfigBuilder(nil).BuildCandidateProjectionFromDB(current, "", false)
	if err != nil || singboxvalidation.ValidateConfig(projection.Config) != nil {
		t.Fatal("restored rebased path candidate cannot complete offline pinned build")
	}
	definitions, err := entitytls.ReadProviderDefinitions(current)
	if err != nil || len(definitions) != 1 || definitions[0].Tag != provider.Tag || definitions[0].RuntimeMode != provider.RuntimeMode {
		t.Fatal("private encrypted provider definition lost portability")
	}
	var sourceOptions, restoredOptions map[string]json.RawMessage
	_ = json.Unmarshal(provider.Options, &sourceOptions)
	_ = json.Unmarshal(definitions[0].Options, &restoredOptions)
	var directory string
	_ = json.Unmarshal(restoredOptions["data_directory"], &directory)
	delete(restoredOptions, "data_directory")
	if !reflect.DeepEqual(sourceOptions, restoredOptions) || !strings.HasPrefix(directory, configstorage.GetDBFolderPath()+string(filepath.Separator)) {
		t.Fatal("provider credentials changed or managed directory was not rebound to the restore target")
	}
	var restored model.Tls
	if err := current.First(&restored, profile.Id).Error; err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(restored.Server, &fields)
	var path string
	_ = json.Unmarshal(fields["certificate_path"], &path)
	if path == paths[0] {
		t.Fatal("archive reused source certificate path")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, cert) {
		t.Fatal("owner-managed certificate publication lost bytes")
	}
}

func TestCertificateEphemeralProjectionInventoryAndCurrentShape(t *testing.T) {
	db := certificateProviderDB(t)
	cert, key := makeSelfSignedCertPEM(t, time.Now().Add(time.Hour))
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", root)
	certPath, keyPath := filepath.Join(t.TempDir(), "cert.pem"), filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(certPath, cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	server, _ := json.Marshal(map[string]any{"enabled": true, "certificate_path": certPath, "key_path": keyPath})
	row := model.Tls{Name: "paths", Server: server, Client: json.RawMessage(`{}`)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	files, err := exportCertificateFiles(context.Background(), db)
	if err != nil || len(files) != 2 {
		t.Fatal("bounded file inventory unavailable")
	}
	if err := restoreCertificateFiles(context.Background(), db, files, false); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"inbounds":[{"type":"trojan","tag":"fixture","tls":` + string(row.Server) + `}]}`)
	projected, err := projectCertificateRuntimeFiles(context.Background(), db, files, config)
	if err != nil || singboxvalidation.ValidateConfig(projected) != nil {
		t.Fatal("verified ephemeral file facts cannot validate complete runtime")
	}
	if _, err := os.Stat(filepath.Join(root, "certs")); !os.IsNotExist(err) {
		t.Fatal("ephemeral projection published files")
	}
	// A custom nested object is not a TLS consumer, even when it happens to
	// contain the same owned path. File projection must not rewrite its bytes.
	custom, _ := json.Marshal(map[string]any{"tls": json.RawMessage(row.Server)})
	consumer, _ := json.Marshal(map[string]any{"tls": json.RawMessage(row.Server), "custom": json.RawMessage(custom)})
	withCustom, _ := json.Marshal(map[string]any{"inbounds": []json.RawMessage{consumer}})
	customProjection, err := projectCertificateRuntimeFiles(context.Background(), db, files, withCustom)
	if err != nil {
		t.Fatal(err)
	}
	var projectedRoot struct {
		Inbounds []map[string]json.RawMessage `json:"inbounds"`
	}
	if json.Unmarshal(customProjection, &projectedRoot) != nil || string(projectedRoot.Inbounds[0]["custom"]) != string(custom) {
		t.Fatal("file projection traversed non-consumer custom data")
	}
	for _, invalid := range [][]backup.OwnerFile{files[:1], append(append([]backup.OwnerFile{}, files...), files[0]), {{Owner: "certificates", Key: "unrecognized", Data: cert}}} {
		if _, err := projectCertificateRuntimeFiles(context.Background(), db, invalid, config); err == nil {
			t.Fatal("unverified inventory bypassed semantic file owner")
		}
	}
}
