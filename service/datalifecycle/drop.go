package datalifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	componentregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	operationcoordination "github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	pressureService "github.com/MalenkiySolovey/solovey-ui/service/resourcepressure"
	"gorm.io/gorm"
)

var (
	ErrPreviewChanged    = errors.New("drop data preview changed")
	ErrOperationConflict = errors.New("data lifecycle operation conflict")
	ErrBlocked           = errors.New("drop data operation is blocked")
	ErrRevisionMismatch  = errors.New("data lifecycle revision mismatch")
	ErrRecoveryRequired  = errors.New("data lifecycle recovery is required")
)

type Resource struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Rows     int64  `json:"rows,omitempty"`
	Owner    string `json:"owner"`
	Terminal string `json:"terminalState,omitempty"`
	Class    string `json:"class,omitempty"`
}

type Preview struct {
	Schema            string     `json:"schema"`
	OwnerID           string     `json:"ownerId"`
	Installed         bool       `json:"installed"`
	Available         bool       `json:"available"`
	Enabled           bool       `json:"enabled"`
	Resources         []Resource `json:"resources"`
	Operations        []string   `json:"operations"`
	LeaseCount        int64      `json:"leaseCount"`
	ExternalAuthority string     `json:"externalAuthority"`
	BackupRequired    bool       `json:"backupRequired"`
	Blockers          []string   `json:"blockers"`
	Postcondition     string     `json:"postcondition"`
	Revision          string     `json:"revision"`
	GeneratedAt       int64      `json:"generatedAt"`
}

type ExecuteRequest struct {
	OwnerID                 string
	ExpectedPreviewRevision string
	IdempotencyKey          string
	Confirmation            string
	BackupAcknowledged      bool
}

type Manager struct {
	mu      sync.Mutex
	DB      func() *gorm.DB
	Now     func() time.Time
	Drop    func(context.Context, string) error
	Root    string
	Enabled func(componentmanifest.Manifest) (bool, error)
	Admit   func(string) bool
	Backup  func(context.Context, model.DataLifecycleOperation) (string, error)
}

var shared = NewManager()

func Shared() *Manager { return shared }

func NewManager() *Manager {
	return &Manager{DB: dbsqlite.DB, Now: time.Now, Drop: coreservice.DropComponentData,
		Root:    filepath.Join(configstorage.GetDBFolderPath(), "recovery", "drop-data"),
		Enabled: enabledstate.Enabled, Admit: func(class string) bool { return pressureService.Shared().Admission(class).Allowed }}
}

func (m *Manager) Preview(ctx context.Context, ownerID string) (Preview, error) {
	if m == nil || !safeOwnerID(ownerID) {
		return Preview{}, errors.New("invalid durable owner")
	}
	now := m.Now().UTC()
	preview := Preview{Schema: "solovey.drop-data-preview/v1", OwnerID: ownerID, BackupRequired: true,
		Resources: []Resource{}, Operations: []string{}, Blockers: []string{}, Postcondition: "OWNER_DATA_ABSENT",
		ExternalAuthority: "NOT_DECLARED", GeneratedAt: now.Unix()}
	installed, err := installstate.InstalledComponents()
	if err != nil {
		preview.Blockers = append(preview.Blockers, "installed_owner_manifest_unavailable")
		preview.Revision = previewRevision(preview)
		return preview, nil
	}
	for _, owner := range installed {
		if owner.ID == ownerID {
			preview.Installed = true
			break
		}
	}
	if !preview.Installed {
		preview.Blockers = append(preview.Blockers, "owner_not_installed")
	}
	component, available := componentregistry.ComponentByID(ownerID)
	preview.Available = available
	if !available {
		preview.Blockers = append(preview.Blockers, "installed_owner_unavailable")
		preview.ExternalAuthority = "UNVERIFIED"
		preview.Revision = previewRevision(preview)
		return preview, nil
	}
	preview.Enabled, err = m.enabled(component.Manifest)
	if err != nil {
		preview.Blockers = append(preview.Blockers, "owner_enabled_state_unavailable")
	} else if preview.Enabled {
		preview.Blockers = append(preview.Blockers, "owner_is_enabled")
	}
	if _, ok := component.Lifecycle.(lifecycle.DataDropper); !ok {
		preview.Blockers = append(preview.Blockers, "owner_drop_contract_unavailable")
	}
	ownerManifest, manifestAvailable := durableowner.Lookup(ownerID)
	if !manifestAvailable {
		preview.Blockers = append(preview.Blockers, "durable_resource_manifest_unavailable")
		preview.Revision = previewRevision(preview)
		return preview, nil
	}
	if db := m.database(); db != nil {
		resources := ownerManifest.Database
		for _, table := range resources.Tables {
			resource := Resource{ID: "table:" + table, Kind: "sqlite_table", Owner: ownerID, Class: resources.RedactionClass, Terminal: "ABSENT"}
			if db.Migrator().HasTable(table) {
				resource.Terminal = "PRESENT"
				if countErr := db.WithContext(ctx).Table(table).Count(&resource.Rows).Error; countErr != nil {
					preview.Blockers = append(preview.Blockers, "owner_table_observation_failed:"+table)
				}
				var indexes []struct{ Name string }
				if indexErr := db.WithContext(ctx).Raw("SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? ORDER BY name", table).Scan(&indexes).Error; indexErr != nil {
					preview.Blockers = append(preview.Blockers, "owner_index_observation_failed:"+table)
				} else {
					for _, index := range indexes {
						preview.Resources = append(preview.Resources, Resource{ID: "index:" + index.Name, Kind: "sqlite_index", Rows: 1,
							Owner: ownerID, Class: resources.RedactionClass, Terminal: "PRESENT"})
					}
				}
			}
			preview.Resources = append(preview.Resources, resource)
		}
		for _, setting := range resources.Settings {
			resource, settingErr := observeSetting(ctx, db, ownerID, setting, "setting", resources.RedactionClass)
			preview.Resources = append(preview.Resources, resource)
			if settingErr != nil {
				preview.Blockers = append(preview.Blockers, "owner_setting_observation_failed:"+setting)
			}
		}
		for _, secret := range resources.Secrets {
			resource, secretErr := observeSetting(ctx, db, ownerID, secret, "secret", "SENSITIVE")
			preview.Resources = append(preview.Resources, resource)
			if secretErr != nil {
				preview.Blockers = append(preview.Blockers, "owner_secret_observation_failed:"+secret)
			}
		}
		for _, file := range resources.Files {
			resource, fileErr := m.observeFile(file.Path, ownerID, file.BackupClass)
			preview.Resources = append(preview.Resources, resource)
			if fileErr != nil {
				preview.Blockers = append(preview.Blockers, "owner_file_observation_failed:"+file.Path)
			}
		}
		migrationResources, migrationErr := observeMigrationRows(ctx, db, ownerID)
		preview.Resources = append(preview.Resources, migrationResources...)
		if migrationErr != nil {
			preview.Blockers = append(preview.Blockers, "owner_migration_observation_failed")
		}
		leaseCount, leaseErr := activeOwnerLeases(db.WithContext(ctx), ownerID, now)
		preview.LeaseCount = leaseCount
		if leaseErr != nil {
			preview.Blockers = append(preview.Blockers, "owner_lease_observation_failed")
		} else if preview.LeaseCount > 0 {
			preview.Blockers = append(preview.Blockers, "active_owner_leases")
		}
		if blocker := m.globalOperationBlocker(ctx); blocker != "" {
			preview.Operations = append(preview.Operations, blocker)
			preview.Blockers = append(preview.Blockers, blocker)
		}
	} else {
		preview.Blockers = append(preview.Blockers, "database_unavailable")
	}
	externalState, externalBlockers := externalAuthority(ctx, component, m.database(), now)
	preview.ExternalAuthority = externalState
	preview.Blockers = append(preview.Blockers, externalBlockers...)
	if preview.ExternalAuthority == "UNAVAILABLE" {
		preview.Blockers = append(preview.Blockers, "external_authority_unavailable")
	}
	sort.Slice(preview.Resources, func(i, j int) bool { return preview.Resources[i].ID < preview.Resources[j].ID })
	sort.Strings(preview.Operations)
	preview.Blockers = uniqueSorted(preview.Blockers)
	preview.Revision = previewRevision(preview)
	return preview, nil
}

func (m *Manager) Execute(ctx context.Context, request ExecuteRequest) (model.DataLifecycleOperation, error) {
	if m == nil || !safeOwnerID(request.OwnerID) || !validDigest(request.ExpectedPreviewRevision) ||
		!safeID(request.IdempotencyKey, 96) || request.Confirmation != "DROP_DATA_"+strings.ToUpper(strings.ReplaceAll(request.OwnerID, "-", "_")) ||
		!request.BackupAcknowledged {
		return model.DataLifecycleOperation{}, errors.New("invalid drop data request")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, err := m.byIdempotency(ctx, request.IdempotencyKey); err == nil {
		if existing.OwnerID == request.OwnerID && existing.ExpectedRevision == request.ExpectedPreviewRevision {
			return existing, nil
		}
		return existing, ErrPreviewChanged
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DataLifecycleOperation{}, err
	}
	preview, err := m.Preview(ctx, request.OwnerID)
	if err != nil {
		return model.DataLifecycleOperation{}, err
	}
	if preview.Revision != request.ExpectedPreviewRevision {
		return model.DataLifecycleOperation{}, ErrPreviewChanged
	}
	if len(preview.Blockers) > 0 {
		return model.DataLifecycleOperation{}, ErrBlocked
	}
	if !m.admitted("heavy_mutation") {
		return model.DataLifecycleOperation{}, ErrOperationConflict
	}
	if blocker := m.globalOperationBlocker(ctx); blocker != "" {
		return model.DataLifecycleOperation{}, ErrOperationConflict
	}
	now := m.Now().UTC()
	operation := model.DataLifecycleOperation{OperationID: "data-operation:" + semanticDigest(struct{ Key, Revision string }{request.IdempotencyKey, preview.Revision})[:48],
		IdempotencyKey: request.IdempotencyKey, Kind: "DROP_DATA", State: "ADMITTED", OwnerID: request.OwnerID,
		ManifestDigest: preview.Revision, ExpectedRevision: request.ExpectedPreviewRevision, Revision: 1,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	if err := operationcoordination.SerializeAdmission(func() error {
		if blocker := m.globalOperationBlocker(ctx); blocker != "" {
			return ErrOperationConflict
		}
		return m.create(ctx, operation, "drop_data_admitted", "")
	}); err != nil {
		return model.DataLifecycleOperation{}, err
	}
	operation, err = m.advance(ctx, operation, "BACKING_UP", "pre_drop_backup_started", "")
	if err != nil {
		return operation, err
	}
	backupRef, err := m.backup(ctx, operation)
	if err != nil || !validDigest(backupRef) {
		if err == nil {
			err = errors.New("pre-drop backup reference is invalid")
		}
		return m.fail(ctx, operation, "pre_drop_backup_failed", err)
	}
	operation.BackupRef = backupRef
	operation, err = m.advance(ctx, operation, "BACKUP_READY", "pre_drop_backup_ready", "")
	if err != nil {
		return operation, err
	}
	operation, err = m.advance(ctx, operation, "DROPPING", "owner_drop_started", "")
	if err != nil {
		return operation, err
	}
	if err := m.Drop(ctx, request.OwnerID); err != nil {
		return m.recovery(ctx, operation, "owner_drop_failed", err)
	}
	operation, err = m.advance(ctx, operation, "VERIFYING", "owner_drop_verifying", "")
	if err != nil {
		return operation, err
	}
	post, err := m.Preview(ctx, request.OwnerID)
	if err != nil || !dropPostcondition(post) {
		return m.recovery(ctx, operation, "owner_drop_postcondition_failed", err)
	}
	return m.advance(ctx, operation, "APPLIED", "owner_data_dropped", "")
}

func (m *Manager) Operation(ctx context.Context, id string) (model.DataLifecycleOperation, error) {
	if !safeID(id, 96) || !strings.HasPrefix(id, "data-operation:") {
		return model.DataLifecycleOperation{}, errors.New("invalid data lifecycle operation id")
	}
	db := m.database()
	if db == nil {
		return model.DataLifecycleOperation{}, errors.New("data lifecycle database unavailable")
	}
	var row model.DataLifecycleOperation
	err := db.WithContext(ctx).First(&row, "operation_id = ?", id).Error
	if err == nil && !validPersistedDataOperation(row) {
		return dataOperationRecoveryProjection(row), nil
	}
	return row, err
}

func (m *Manager) Recovery(ctx context.Context) (model.DataLifecycleOperation, error) {
	db := m.database()
	if db == nil {
		return model.DataLifecycleOperation{}, errors.New("data lifecycle database unavailable")
	}
	var row model.DataLifecycleOperation
	err := db.WithContext(ctx).Where("state = ? OR restored_untrusted = ?", "RECOVERY_REQUIRED", true).Order("updated_at DESC").First(&row).Error
	if err == nil {
		if !validPersistedDataOperation(row) {
			row = dataOperationRecoveryProjection(row)
		}
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return row, err
	}
	var recent []model.DataLifecycleOperation
	if err := db.WithContext(ctx).Order("updated_at DESC, operation_id DESC").Limit(maxStartupReconciliations).Find(&recent).Error; err != nil {
		return row, err
	}
	for _, candidate := range recent {
		if !validPersistedDataOperation(candidate) {
			return dataOperationRecoveryProjection(candidate), nil
		}
	}
	return row, err
}

func (m *Manager) preDropBackup(ctx context.Context, operation model.DataLifecycleOperation) (string, error) {
	path, cleanup, err := dbbackup.PrepareExportContext(ctx, "")
	if err != nil {
		return "", err
	}
	defer cleanup()
	if err := os.MkdirAll(m.Root, 0o700); err != nil {
		return "", err
	}
	destination := filepath.Join(m.Root, operation.OperationID+".db")
	temporary := destination + ".partial"
	_ = os.Remove(temporary)
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	written, copyErr := copyWithContext(ctx, io.MultiWriter(output, hash), io.LimitReader(input, 512<<20+1))
	syncErr, closeErr := output.Sync(), output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written > 512<<20 {
		_ = os.Remove(temporary)
		return "", errors.Join(copyErr, syncErr, closeErr, errors.New("pre-drop backup exceeded bounds"))
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (m *Manager) database() *gorm.DB {
	if m == nil || m.DB == nil {
		return nil
	}
	return m.DB()
}

func (m *Manager) enabled(item componentmanifest.Manifest) (bool, error) {
	if m.Enabled == nil {
		return enabledstate.Enabled(item)
	}
	return m.Enabled(item)
}

func (m *Manager) admitted(class string) bool {
	if m.Admit == nil {
		return pressureService.Shared().Admission(class).Allowed
	}
	return m.Admit(class)
}

func (m *Manager) backup(ctx context.Context, operation model.DataLifecycleOperation) (string, error) {
	if m.Backup != nil {
		return m.Backup(ctx, operation)
	}
	return m.preDropBackup(ctx, operation)
}

func (m *Manager) globalOperationBlocker(ctx context.Context) string {
	db := m.database()
	return operationcoordination.Blocker(ctx, db, "")
}

func (m *Manager) byIdempotency(ctx context.Context, key string) (model.DataLifecycleOperation, error) {
	var row model.DataLifecycleOperation
	db := m.database()
	if db == nil {
		return row, errors.New("data lifecycle database unavailable")
	}
	err := db.WithContext(ctx).First(&row, "idempotency_key = ?", key).Error
	return row, err
}

func (m *Manager) create(ctx context.Context, operation model.DataLifecycleOperation, event, reason string) error {
	return m.database().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		return tx.Create(&model.DataLifecycleJournal{OperationID: operation.OperationID, State: operation.State,
			Event: event, ReasonCode: reason, Revision: operation.Revision, CreatedAt: operation.UpdatedAt}).Error
	})
}

func (m *Manager) advance(ctx context.Context, operation model.DataLifecycleOperation, state, event, reason string) (model.DataLifecycleOperation, error) {
	previousState, previousRevision := operation.State, operation.Revision
	operation.State, operation.ReasonCode, operation.Revision, operation.UpdatedAt = state, reason, operation.Revision+1, m.Now().Unix()
	err := m.database().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.DataLifecycleOperation{}).Where("operation_id = ? AND revision = ? AND state = ?", operation.OperationID, previousRevision, previousState).
			Updates(map[string]any{"state": operation.State, "reason_code": operation.ReasonCode, "revision": operation.Revision,
				"manifest_digest": operation.ManifestDigest, "backup_ref": operation.BackupRef, "updated_at": operation.UpdatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionMismatch
		}
		return tx.Create(&model.DataLifecycleJournal{OperationID: operation.OperationID, State: operation.State,
			Event: event, ReasonCode: reason, Revision: operation.Revision, CreatedAt: operation.UpdatedAt}).Error
	})
	return operation, err
}

func (m *Manager) fail(ctx context.Context, operation model.DataLifecycleOperation, reason string, cause error) (model.DataLifecycleOperation, error) {
	event := strings.ToLower(operation.Kind) + "_failed"
	failed, err := m.advance(ctx, operation, "FAILED", event, reason)
	if err != nil {
		return failed, err
	}
	return failed, cause
}

func (m *Manager) recovery(ctx context.Context, operation model.DataLifecycleOperation, reason string, cause error) (model.DataLifecycleOperation, error) {
	event := strings.ToLower(operation.Kind) + "_recovery_required"
	recovery, err := m.advance(ctx, operation, "RECOVERY_REQUIRED", event, reason)
	if err != nil {
		return recovery, err
	}
	if cause == nil {
		cause = ErrRecoveryRequired
	}
	return recovery, cause
}

func activeOwnerLeases(db *gorm.DB, ownerID string, now time.Time) (int64, error) {
	if db == nil || !db.Migrator().HasTable(&model.InboundEndpointLease{}) {
		return 0, nil
	}
	var count int64
	err := db.Model(&model.InboundEndpointLease{}).Where("(provider_id = ? OR holder_id = ? OR resource_id LIKE ?) AND released_at_unix = 0 AND expires_at_unix > ?",
		ownerID, ownerID, ownerID+":%", now.Unix()).Count(&count).Error
	return count, err
}

func externalAuthority(ctx context.Context, component componentregistry.Component, db *gorm.DB, now time.Time) (string, []string) {
	inspector, ok := component.Lifecycle.(lifecycle.DropAuthorityInspector)
	if !ok {
		return "NOT_REQUIRED", []string{}
	}
	status := inspector.InspectDropAuthority(ctx, db, now)
	if status.State == "" {
		return "UNAVAILABLE", []string{"external_authority_contract_invalid"}
	}
	return status.State, append([]string(nil), status.ReasonCodes...)
}

func dropPostcondition(preview Preview) bool {
	for _, resource := range preview.Resources {
		if resource.Rows != 0 {
			return false
		}
	}
	return preview.LeaseCount == 0 && (preview.ExternalAuthority == "NOT_REQUIRED" || preview.ExternalAuthority == "VERIFIED_SAFE")
}

func observeSetting(ctx context.Context, db *gorm.DB, ownerID, key, kind, class string) (Resource, error) {
	resource := Resource{ID: kind + ":" + key, Kind: kind, Owner: ownerID, Class: class, Terminal: "ABSENT"}
	if db == nil || !db.Migrator().HasTable(&model.Setting{}) {
		return resource, nil
	}
	if err := db.WithContext(ctx).Model(&model.Setting{}).Where("key = ?", key).Count(&resource.Rows).Error; err != nil {
		return resource, err
	}
	if resource.Rows > 0 {
		resource.Terminal = "PRESENT"
	}
	return resource, nil
}

func observeMigrationRows(ctx context.Context, db *gorm.DB, ownerID string) ([]Resource, error) {
	result := []Resource{}
	var joined error
	for _, item := range []struct {
		model any
		id    string
		where string
		args  []any
	}{
		{&model.ComponentMigration{}, "migration:component", "component_id = ?", []any{ownerID}},
		{&model.MigrationJournal{}, "migration:journal", "scope = ? AND owner_id = ?", []any{"component", ownerID}},
	} {
		resource := Resource{ID: item.id, Kind: "migration_record", Owner: ownerID, Class: "CONFIDENTIAL", Terminal: "ABSENT"}
		if db.Migrator().HasTable(item.model) {
			if err := db.WithContext(ctx).Model(item.model).Where(item.where, item.args...).Count(&resource.Rows).Error; err != nil {
				joined = errors.Join(joined, err)
			} else if resource.Rows > 0 {
				resource.Terminal = "PRESENT"
			}
		}
		result = append(result, resource)
	}
	return result, joined
}

func (m *Manager) observeFile(relativePath, ownerID, class string) (Resource, error) {
	resource := Resource{ID: "file:" + relativePath, Kind: "file_tree", Owner: ownerID, Class: class, Terminal: "ABSENT"}
	base, err := filepath.Abs(filepath.Dir(configstorage.GetDBFolderPath()))
	if err != nil {
		return resource, err
	}
	target, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(relativePath)))
	if err != nil {
		return resource, err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return resource, errors.New("declared owner file escapes the data root")
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return resource, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return resource, errors.Join(err, errors.New("declared owner file is not inspectable"))
	}
	resource.Terminal = "PRESENT"
	if !info.IsDir() {
		resource.Rows = 1
		return resource, nil
	}
	err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("declared owner tree contains a symlink")
		}
		resource.Rows++
		if resource.Rows > 10000 {
			return errors.New("declared owner tree exceeds preview bounds")
		}
		return nil
	})
	return resource, err
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil || written != count {
				return total, errors.Join(writeErr, io.ErrShortWrite)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func previewRevision(preview Preview) string {
	copyPreview := preview
	copyPreview.Revision, copyPreview.GeneratedAt = "", 0
	return semanticDigest(copyPreview)
}

func semanticDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func safeOwnerID(value string) bool {
	if value == "core" {
		return false
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

func safeID(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._:@+-", char) {
			continue
		}
		return false
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validPersistedDataOperation(operation model.DataLifecycleOperation) bool {
	if !safeID(operation.OperationID, 96) || !strings.HasPrefix(operation.OperationID, "data-operation:") ||
		!safeID(operation.IdempotencyKey, 96) || operation.Revision == 0 ||
		!validDigest(operation.ExpectedRevision) ||
		(operation.ManifestDigest != "" && !validDigest(operation.ManifestDigest)) ||
		(operation.BackupRef != "" && !validDigest(operation.BackupRef)) ||
		(operation.ReasonCode != "" && !safeID(operation.ReasonCode, 96)) ||
		operation.CreatedAt <= 0 || operation.UpdatedAt < operation.CreatedAt {
		return false
	}
	switch operation.Kind {
	case "RESTORE":
		if operation.OwnerID != "core" {
			return false
		}
		switch operation.State {
		case "ADMITTED", "RESTORING", "FAILED", "ROLLED_BACK", "RECOVERY_REQUIRED":
			return true
		case "APPLIED":
			return validDigest(operation.BackupRef)
		default:
			return false
		}
	case "DROP_DATA":
		if !safeOwnerID(operation.OwnerID) || !validDigest(operation.ManifestDigest) {
			return false
		}
		switch operation.State {
		case "ADMITTED", "BACKING_UP", "FAILED", "ROLLED_BACK", "RECOVERY_REQUIRED":
			return true
		case "BACKUP_READY", "DROPPING", "VERIFYING", "APPLIED":
			return validDigest(operation.BackupRef)
		default:
			return false
		}
	default:
		return false
	}
}

func dataOperationRecoveryProjection(operation model.DataLifecycleOperation) model.DataLifecycleOperation {
	operation.State = "RECOVERY_REQUIRED"
	operation.ReasonCode = "data_lifecycle_operation_state_invalid"
	operation.RestoredUntrusted = true
	return operation
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func ReasonCode(err error) string {
	switch {
	case errors.Is(err, ErrPreviewChanged):
		return "drop_data_preview_changed"
	case errors.Is(err, ErrBlocked):
		return "drop_data_blocked"
	case errors.Is(err, ErrOperationConflict):
		return "data_lifecycle_operation_conflict"
	case errors.Is(err, ErrRevisionMismatch):
		return "data_lifecycle_revision_mismatch"
	case errors.Is(err, ErrRecoveryRequired):
		return "data_lifecycle_recovery_required"
	default:
		return "data_lifecycle_failed"
	}
}
