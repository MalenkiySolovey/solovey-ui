package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/database/sqliteident"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"gorm.io/gorm"
)

const (
	BackupManifestSchema = "solovey.backup/v1"
	BackupManifestTable  = "backup_manifest_v1"
	MaxBackupTables      = 512
	MaxBackupManifest    = 2 << 20
)

type backupManifestRecord struct {
	Scope   string `gorm:"primaryKey;size:16"`
	Payload []byte `gorm:"not null"`
	Digest  string `gorm:"size:64;not null"`
}

func (backupManifestRecord) TableName() string { return BackupManifestTable }

type BackupTableManifest struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	Rows          int64  `json:"rows"`
	SchemaDigest  string `json:"schemaDigest"`
	ContentDigest string `json:"contentDigest"`
	Excluded      bool   `json:"excluded"`
	ExclusionCode string `json:"exclusionCode,omitempty"`
}

type BackupOwnerManifest struct {
	ID               string                                       `json:"id"`
	Installed        bool                                         `json:"installed"`
	Available        bool                                         `json:"available"`
	Mode             string                                       `json:"mode"`
	ResourceManifest *componentmanifest.DurableResourceManifestV1 `json:"resourceManifest,omitempty"`
}

type BackupManifest struct {
	Schema             string                `json:"schema"`
	BackupID           string                `json:"backupId"`
	CreatedAt          int64                 `json:"createdAt"`
	AppVersion         string                `json:"appVersion"`
	CoreSchema         string                `json:"coreSchema"`
	SQLiteModule       string                `json:"sqliteModule"`
	SQLiteRuntime      string                `json:"sqliteRuntime"`
	SQLiteSourceID     string                `json:"sqliteSourceId"`
	ReleaseSequence    uint64                `json:"releaseSequence"`
	ReleaseVersion     string                `json:"releaseVersion"`
	DeploymentProfile  string                `json:"deploymentProfile"`
	DeploymentRevision string                `json:"deploymentRevision"`
	Encryption         string                `json:"encryption"`
	MaxBytes           int64                 `json:"maxBytes"`
	Owners             []BackupOwnerManifest `json:"owners"`
	Tables             []BackupTableManifest `json:"tables"`
	Compatibility      string                `json:"compatibility"`
}

func writeBackupManifest(ctx context.Context, db *gorm.DB, tables []backupTable, excluded map[string]bool) error {
	if ctx == nil || db == nil || len(tables) == 0 || len(tables) > MaxBackupTables {
		return errors.New("backup manifest input is invalid")
	}
	runtimeStatus, err := dbsqlite.InspectRuntime(db)
	if err != nil {
		return err
	}
	manifest := BackupManifest{Schema: BackupManifestSchema, CreatedAt: time.Now().UTC().Unix(), AppVersion: configidentity.GetVersion(),
		CoreSchema: "1.11", SQLiteModule: dbsqlite.SQLiteModuleVersion, SQLiteRuntime: runtimeStatus.RuntimeVersion,
		SQLiteSourceID: runtimeStatus.SourceID, Encryption: "INNER_PLAINTEXT", MaxBytes: 512 << 20,
		Compatibility: "EXACT_OR_FORWARD_MIGRATABLE", Owners: []BackupOwnerManifest{{ID: "core", Installed: true, Available: true, Mode: "TYPED"}}}
	ownerModes := map[string]string{"core": "TYPED"}
	ownerAvailability := map[string]bool{"core": true}
	installedOwners, err := installstate.InstalledComponents()
	if err != nil {
		return fmt.Errorf("installed owner inventory is unavailable: %w", err)
	}
	for _, owner := range installedOwners {
		item, available := durableowner.Lookup(owner.ID)
		if !available {
			return fmt.Errorf("installed durable owner %q is unavailable; manifest fails closed", owner.ID)
		}
		resources := item.Database.Normalized(item.Version)
		mode := "TYPED"
		if !resources.Declared() {
			mode = "NO_DURABLE_DATA"
		}
		ownerModes[owner.ID], ownerAvailability[owner.ID] = mode, true
		manifest.Owners = append(manifest.Owners, BackupOwnerManifest{ID: owner.ID, Installed: true, Available: true,
			Mode: mode, ResourceManifest: &resources})
	}
	for _, table := range tables {
		if table.alwaysExclude {
			schemaDigest, contentDigest := excludedTableDigests(table.name, table.exclusionCode)
			manifest.Tables = append(manifest.Tables, BackupTableManifest{Owner: "core", Name: table.name, Rows: 0,
				SchemaDigest: schemaDigest, ContentDigest: contentDigest, Excluded: true, ExclusionCode: table.exclusionCode})
			continue
		}
		if !db.Migrator().HasTable(table.name) {
			continue
		}
		owner := table.owner
		if owner == "" {
			owner = "core"
		}
		mode := "TYPED"
		if table.opaque {
			mode = "OPAQUE_PRESERVED"
		}
		if ownerModes[owner] != "OPAQUE_PRESERVED" {
			ownerModes[owner] = mode
		}
		if owner == "core" {
			ownerAvailability[owner] = true
		} else if _, available := durableowner.Lookup(owner); available {
			ownerAvailability[owner] = true
		}
		entry, err := digestBackupTable(ctx, db, owner, table.name)
		if err != nil {
			return err
		}
		entry.Excluded = excluded[table.name]
		if entry.Excluded {
			entry.ExclusionCode = "OPERATOR_EXCLUDED_OPTIONAL_TELEMETRY"
			if entry.Rows != 0 {
				return fmt.Errorf("excluded backup table %s is not empty", table.name)
			}
		}
		manifest.Tables = append(manifest.Tables, entry)
	}
	for index := range manifest.Owners {
		owner := &manifest.Owners[index]
		owner.Mode, owner.Available = ownerModes[owner.ID], ownerAvailability[owner.ID]
	}
	declaredOwners := make(map[string]struct{}, len(manifest.Owners))
	for _, owner := range manifest.Owners {
		declaredOwners[owner.ID] = struct{}{}
	}
	for _, table := range manifest.Tables {
		if _, exists := declaredOwners[table.Owner]; !exists {
			return fmt.Errorf("backup table %q has no installed durable owner %q", table.Name, table.Owner)
		}
	}
	if db.Migrator().HasTable(&model.UpdateReleaseState{}) {
		var releaseState model.UpdateReleaseState
		if err := db.Order("last_applied_sequence DESC").Limit(1).Find(&releaseState).Error; err == nil && releaseState.Channel != "" {
			manifest.ReleaseSequence, manifest.ReleaseVersion = releaseState.LastAppliedSequence, releaseState.Version
		}
	}
	if db.Migrator().HasTable(&model.DeploymentState{}) {
		var deployment model.DeploymentState
		if err := db.Limit(1).Find(&deployment).Error; err == nil && deployment.Scope != "" {
			manifest.DeploymentProfile, manifest.DeploymentRevision = deployment.ActiveProfile, deployment.PostureRevision
		}
	}
	sort.Slice(manifest.Owners, func(i, j int) bool { return manifest.Owners[i].ID < manifest.Owners[j].ID })
	sort.Slice(manifest.Tables, func(i, j int) bool { return manifest.Tables[i].Name < manifest.Tables[j].Name })
	manifest.BackupID = backupManifestDigest(manifest)
	payload, err := json.Marshal(manifest)
	if err != nil || len(payload) > MaxBackupManifest {
		return errors.New("backup manifest exceeds bounds")
	}
	if err := db.AutoMigrate(&backupManifestRecord{}); err != nil {
		return err
	}
	return db.Create(&backupManifestRecord{Scope: "backup", Payload: payload, Digest: manifest.BackupID}).Error
}

func LoadAndVerifyManifest(ctx context.Context, db *gorm.DB) (BackupManifest, error) {
	if ctx == nil || db == nil || !db.Migrator().HasTable(BackupManifestTable) {
		return BackupManifest{}, errors.New("backup manifest is absent")
	}
	var record backupManifestRecord
	if err := db.WithContext(ctx).First(&record, "scope = ?", "backup").Error; err != nil {
		return BackupManifest{}, err
	}
	if len(record.Payload) == 0 || len(record.Payload) > MaxBackupManifest || len(record.Digest) != 64 {
		return BackupManifest{}, errors.New("backup manifest record is invalid")
	}
	var manifest BackupManifest
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return BackupManifest{}, errors.New("backup manifest JSON is invalid")
	}
	if manifest.Schema != BackupManifestSchema || manifest.BackupID != record.Digest || backupManifestDigest(manifest) != record.Digest ||
		manifest.CoreSchema == "" || len(manifest.Tables) == 0 || len(manifest.Tables) > MaxBackupTables || len(manifest.Owners) == 0 {
		return BackupManifest{}, errors.New("backup manifest identity is invalid")
	}
	seen := map[string]struct{}{}
	owners := map[string]struct{}{}
	for _, owner := range manifest.Owners {
		if !safeOwnerName(owner.ID) || !owner.Installed || !owner.Available || !validBackupOwnerMode(owner.ID, owner.Mode) {
			return BackupManifest{}, errors.New("backup owner manifest is invalid")
		}
		if _, duplicate := owners[owner.ID]; duplicate {
			return BackupManifest{}, errors.New("backup owner manifest is duplicated")
		}
		owners[owner.ID] = struct{}{}
		if owner.ID != "core" {
			if owner.ResourceManifest == nil ||
				owner.ResourceManifest.Validate(owner.ID, owner.ResourceManifest.SchemaVersion) != nil ||
				owner.ResourceManifest.Declared() &&
					(owner.ResourceManifest.ContractChecksum() != owner.ResourceManifest.SchemaChecksum || owner.Mode == "NO_DURABLE_DATA") ||
				!owner.ResourceManifest.Declared() && owner.Mode != "NO_DURABLE_DATA" {
				return BackupManifest{}, errors.New("backup durable owner manifest is invalid")
			}
		}
	}
	for _, table := range manifest.Tables {
		if !sqliteident.Valid(table.Name) || !safeOwnerName(table.Owner) || !validHexDigest(table.SchemaDigest) || !validHexDigest(table.ContentDigest) {
			return BackupManifest{}, errors.New("backup table manifest is invalid")
		}
		if _, duplicate := seen[table.Name]; duplicate {
			return BackupManifest{}, errors.New("backup table manifest is duplicated")
		}
		seen[table.Name] = struct{}{}
		if _, exists := owners[table.Owner]; !exists {
			return BackupManifest{}, errors.New("backup table owner is absent")
		}
		if table.Excluded && (!validExclusionCode(table.ExclusionCode) || table.Rows != 0) ||
			!table.Excluded && table.ExclusionCode != "" {
			return BackupManifest{}, errors.New("backup table exclusion metadata is invalid")
		}
		if table.Excluded && table.ExclusionCode != "OPERATOR_EXCLUDED_OPTIONAL_TELEMETRY" {
			schemaDigest, contentDigest := excludedTableDigests(table.Name, table.ExclusionCode)
			if table.SchemaDigest != schemaDigest || table.ContentDigest != contentDigest || db.Migrator().HasTable(table.Name) {
				return BackupManifest{}, errors.New("nonportable backup table exclusion is invalid")
			}
			continue
		}
		actual, err := digestBackupTable(ctx, db, table.Owner, table.Name)
		if err != nil || actual.Rows != table.Rows || actual.SchemaDigest != table.SchemaDigest || actual.ContentDigest != table.ContentDigest {
			return BackupManifest{}, fmt.Errorf("backup table %s digest mismatch", table.Name)
		}
	}
	allowedTables := make(map[string]bool, len(seen))
	for name := range seen {
		allowedTables[name] = true
	}
	if err := validateSQLiteObjectInventory(ctx, db, allowedTables, true); err != nil {
		return BackupManifest{}, err
	}
	return manifest, nil
}

func validBackupOwnerMode(ownerID, mode string) bool {
	if ownerID == "core" {
		return mode == "TYPED" || mode == "LEGACY_TYPED"
	}
	return mode == "TYPED" || mode == "NO_DURABLE_DATA" || mode == "OPAQUE_PRESERVED"
}

func validateSQLiteObjectInventory(ctx context.Context, db *gorm.DB, allowedTables map[string]bool, allowManifest bool) error {
	if ctx == nil || db == nil {
		return errors.New("backup schema inventory input is invalid")
	}
	type sqliteObject struct {
		Type string
		Name string
	}
	var objects []sqliteObject
	if err := db.WithContext(ctx).Raw(`
SELECT type, name
FROM sqlite_master
WHERE name NOT LIKE 'sqlite_%' AND type IN ('table', 'view', 'trigger')
ORDER BY type, name
`).Scan(&objects).Error; err != nil {
		return fmt.Errorf("backup schema inventory is unavailable: %w", err)
	}
	if len(objects) == 0 || len(objects) > MaxBackupTables+1 {
		return errors.New("backup schema inventory is outside bounds")
	}
	for _, object := range objects {
		if object.Type != "table" {
			return fmt.Errorf("backup contains unsupported executable schema object %s", object.Type)
		}
		if allowManifest && object.Name == BackupManifestTable {
			continue
		}
		if !allowedTables[object.Name] {
			return fmt.Errorf("backup contains undeclared table %s", object.Name)
		}
	}
	return nil
}

func validExclusionCode(value string) bool {
	switch value {
	case "OPERATOR_EXCLUDED_OPTIONAL_TELEMETRY", "NONPORTABLE_RUNTIME_AUTHORITY", "NONPORTABLE_RUNTIME_STATE", "NONPORTABLE_HOST_AUTHORITY":
		return true
	default:
		return false
	}
}

func excludedTableDigests(name, code string) (string, string) {
	schema := sha256.Sum256([]byte("excluded-schema:" + code + ":" + name))
	content := sha256.Sum256([]byte("excluded-content:" + code + ":" + name))
	return hex.EncodeToString(schema[:]), hex.EncodeToString(content[:])
}

func digestBackupTable(ctx context.Context, db *gorm.DB, owner, tableName string) (BackupTableManifest, error) {
	entry := BackupTableManifest{Owner: owner, Name: tableName}
	var schema string
	if err := db.WithContext(ctx).Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ? LIMIT 1", tableName).Scan(&schema).Error; err != nil || schema == "" {
		return entry, errors.New("backup table schema is unavailable")
	}
	schemaSum := sha256.Sum256([]byte(schema))
	entry.SchemaDigest = hex.EncodeToString(schemaSum[:])
	orderClause, err := deterministicTableOrder(ctx, db, tableName)
	if err != nil {
		return entry, err
	}
	rows, err := db.WithContext(ctx).Raw("SELECT * FROM " + sqliteident.Quote(tableName) + orderClause).Rows()
	if err != nil {
		return entry, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil || len(columns) == 0 || len(columns) > 256 {
		return entry, errors.New("backup table columns are invalid")
	}
	hash := sha256.New()
	writeDigestField(hash, []byte(schema))
	for _, column := range columns {
		writeDigestField(hash, []byte(column))
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return entry, err
		}
		values, targets := make([]any, len(columns)), make([]any, len(columns))
		for index := range values {
			targets[index] = &values[index]
		}
		if err := rows.Scan(targets...); err != nil {
			return entry, err
		}
		for _, value := range values {
			encoded, err := canonicalSQLiteValue(value)
			if err != nil {
				return entry, err
			}
			writeDigestField(hash, encoded)
		}
		entry.Rows++
	}
	if err := rows.Err(); err != nil {
		return entry, err
	}
	entry.ContentDigest = hex.EncodeToString(hash.Sum(nil))
	return entry, nil
}

type sqliteTableColumn struct {
	Name string `gorm:"column:name"`
	PK   int    `gorm:"column:pk"`
}

func deterministicTableOrder(ctx context.Context, db *gorm.DB, tableName string) (string, error) {
	var columns []sqliteTableColumn
	if err := db.WithContext(ctx).Raw("PRAGMA table_info(" + sqliteident.Quote(tableName) + ")").Scan(&columns).Error; err != nil {
		return "", err
	}
	primary := make([]sqliteTableColumn, 0, len(columns))
	for _, column := range columns {
		if column.PK > 0 {
			primary = append(primary, column)
		}
	}
	if len(primary) == 0 {
		// Every ordinary SQLite table has rowid. WITHOUT ROWID tables are
		// required to declare a primary key and take the branch below.
		return " ORDER BY rowid", nil
	}
	sort.Slice(primary, func(i, j int) bool { return primary[i].PK < primary[j].PK })
	order := make([]string, len(primary))
	for index, column := range primary {
		if !sqliteident.Valid(column.Name) {
			return "", errors.New("backup primary-key identity is invalid")
		}
		order[index] = sqliteident.Quote(column.Name)
	}
	return " ORDER BY " + strings.Join(order, ","), nil
}

func canonicalSQLiteValue(value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return []byte{0}, nil
	case int64:
		var data [9]byte
		data[0] = 1
		binary.BigEndian.PutUint64(data[1:], uint64(typed))
		return data[:], nil
	case float64:
		return json.Marshal(struct {
			T string  `json:"t"`
			V float64 `json:"v"`
		}{"float", typed})
	case bool:
		if typed {
			return []byte{2, 1}, nil
		}
		return []byte{2, 0}, nil
	case string:
		return append([]byte{3}, []byte(typed)...), nil
	case []byte:
		return append([]byte{4}, typed...), nil
	case time.Time:
		return append([]byte{5}, []byte(typed.UTC().Format(time.RFC3339Nano))...), nil
	default:
		return nil, fmt.Errorf("unsupported SQLite backup value %T", value)
	}
}

func writeDigestField(writer io.Writer, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = writer.Write(size[:])
	_, _ = writer.Write(value)
}

func backupManifestDigest(manifest BackupManifest) string {
	manifest.BackupID = ""
	data, _ := json.Marshal(manifest)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func safeOwnerName(value string) bool {
	if value == "core" {
		return true
	}
	if value == "" || len(value) > 96 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' {
			continue
		}
		return false
	}
	return true
}
