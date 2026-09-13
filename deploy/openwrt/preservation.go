package openwrt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	"github.com/MalenkiySolovey/solovey-ui/database/restorestate"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
)

const (
	PreservationSchemaV1        = "solovey-ui/openwrt-sysupgrade-preservation/v1"
	PreservationStatePrepared   = "PREPARED"
	PreservationStateRestoring  = "RESTORING"
	PreservationStateRestored   = "RESTORED"
	PreservationStateBackupDone = "BACKUP_COMPLETED"
	PreservationStateBackupFail = "BACKUP_FAILED"
	DefaultPreservationRoot     = DefaultDatabaseFolder + "/sysupgrade-preservation"
	DefaultPreservationSnapshot = DefaultPreservationRoot + "/database.db"
	DefaultPreservationMetadata = DefaultPreservationRoot + "/metadata.json"
	MaxPreservationMetadata     = 16 << 10
)

var (
	ErrPreservationUnavailable = errors.New("OpenWrt sysupgrade preservation is unavailable")
	ErrPreservationAmbiguous   = errors.New("OpenWrt sysupgrade preservation state is ambiguous")
	ErrPreservationChanged     = errors.New("OpenWrt sysupgrade preservation identity changed")
	preservationRootPath       = DefaultPreservationRoot
	preservationSnapshotPath   = DefaultPreservationSnapshot
	preservationMetadataPath   = DefaultPreservationMetadata
)

// PreservationMetadataV1 is the bounded restore authority preserved beside
// the logical database export. Live SQLite files and sidecars are never part
// of this contract.
type PreservationMetadataV1 struct {
	Schema              string `json:"schema"`
	State               string `json:"state"`
	Identity            string `json:"identity"`
	ArtifactPath        string `json:"artifactPath"`
	ArtifactDigest      string `json:"artifactDigest"`
	ArtifactBytes       int64  `json:"artifactBytes"`
	BackupID            string `json:"backupId"`
	RehearsalRevision   string `json:"rehearsalRevision"`
	AppVersion          string `json:"appVersion"`
	CoreSchema          string `json:"coreSchema"`
	DeploymentProfile   string `json:"deploymentProfile,omitempty"`
	DeploymentRevision  string `json:"deploymentRevision,omitempty"`
	PreparedAt          int64  `json:"preparedAt"`
	RestoredAt          int64  `json:"restoredAt,omitempty"`
	RestoreOperationID  string `json:"restoreOperationId,omitempty"`
	RestoreOperationRev uint64 `json:"restoreOperationRevision,omitempty"`
	BackupCompletedAt   int64  `json:"backupCompletedAt,omitempty"`
	BackupFailedAt      int64  `json:"backupFailedAt,omitempty"`
}

// PreservationEnvironment supplies only the deployment facts required to
// prove the fixed preservation destination. It is not a filesystem API.
type PreservationEnvironment struct {
	InspectDurableState func(string, uint64) (DurableStateEvidence, error)
	SyncDirectory       func(string) error
	RemoveFile          func(string) error
	Now                 func() time.Time
}

func (environment PreservationEnvironment) validate() error {
	if environment.InspectDurableState == nil || environment.SyncDirectory == nil || environment.RemoveFile == nil || environment.Now == nil {
		return ErrPreservationUnavailable
	}
	return nil
}

// PrepareSysupgradePreservation creates and verifies the one logical artifact
// that the OpenWrt preservation archive is allowed to carry.
func PrepareSysupgradePreservation(ctx context.Context, profile ProfileV1, environment PreservationEnvironment) (PreservationMetadataV1, error) {
	metadata := PreservationMetadataV1{}
	if ctx == nil || profile.DatabaseFolder != DefaultDatabaseFolder || environment.validate() != nil {
		return metadata, ErrPreservationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return metadata, err
	}
	if err := profile.Validate(); err != nil {
		return metadata, err
	}
	if configstorage.GetDBFolderPath() != profile.DatabaseFolder {
		return metadata, errors.New("OpenWrt database storage authority differs from the deployment profile")
	}
	if err := ensurePreservationRoot(environment.SyncDirectory); err != nil {
		return metadata, err
	}
	if err := reconcileBeforePreservationPreparation(ctx, environment); err != nil {
		return metadata, err
	}

	exportPath, cleanup, err := dbbackup.PrepareExportContext(ctx, "")
	if err != nil {
		return metadata, fmt.Errorf("%w: logical snapshot failed: %v", ErrPreservationUnavailable, err)
	}
	defer cleanup()
	export, err := os.Open(exportPath) // #nosec G304 -- generated bounded backup path.
	if err != nil {
		return metadata, err
	}
	rehearsal, rehearsalErr := dbbackup.Rehearse(ctx, export)
	closeErr := export.Close()
	if rehearsalErr != nil || closeErr != nil || !rehearsal.Possible || rehearsal.ManifestStatus != "VERIFIED" ||
		rehearsal.Manifest == nil || !validPreservationDigest(rehearsal.BackupDigest) ||
		!validPreservationDigest(rehearsal.Manifest.BackupID) || !validPreservationDigest(rehearsal.Revision) ||
		rehearsal.BackupBytes <= 0 || rehearsal.BackupBytes > dbbackup.MaxRestoreBytes {
		return metadata, errors.Join(ErrPreservationUnavailable, rehearsalErr, closeErr, errors.New("logical snapshot rehearsal failed"))
	}
	requiredBytes, err := preservationRequiredBytes(rehearsal.BackupBytes)
	if err != nil {
		return metadata, err
	}
	evidence, err := environment.InspectDurableState(profile.DatabaseFolder, requiredBytes)
	if err != nil {
		return metadata, errors.Join(ErrPreservationUnavailable, err)
	}
	if err := profile.ValidateDurableState(evidence); err != nil {
		return metadata, err
	}

	if err := installPreservationSnapshot(ctx, exportPath, rehearsal, environment.SyncDirectory); err != nil {
		return metadata, err
	}
	manifest := rehearsal.Manifest
	metadata = PreservationMetadataV1{
		Schema: PreservationSchemaV1, State: PreservationStatePrepared, ArtifactPath: preservationSnapshotPath,
		ArtifactDigest: rehearsal.BackupDigest, ArtifactBytes: rehearsal.BackupBytes, BackupID: manifest.BackupID,
		RehearsalRevision: rehearsal.Revision, AppVersion: manifest.AppVersion, CoreSchema: manifest.CoreSchema,
		DeploymentProfile: manifest.DeploymentProfile, DeploymentRevision: manifest.DeploymentRevision,
		PreparedAt: environment.Now().UTC().Unix(),
	}
	metadata.Identity = preservationIdentity(metadata)
	if err := writePreservationMetadata(metadata, environment.SyncDirectory); err != nil {
		return PreservationMetadataV1{}, err
	}
	verified, _, err := LoadSysupgradePreservation(ctx)
	if err != nil || verified.Identity != metadata.Identity {
		return PreservationMetadataV1{}, errors.Join(ErrPreservationChanged, err)
	}
	return verified, nil
}

// LoadSysupgradePreservation validates both the bounded metadata and the
// logical snapshot through the existing restore rehearsal owner.
func LoadSysupgradePreservation(ctx context.Context) (PreservationMetadataV1, dbbackup.RestoreRehearsal, error) {
	rehearsal := dbbackup.RestoreRehearsal{}
	if ctx == nil {
		return PreservationMetadataV1{}, rehearsal, ErrPreservationUnavailable
	}
	metadata, err := loadPreservationMetadata()
	if err != nil {
		return PreservationMetadataV1{}, rehearsal, err
	}
	snapshotPresent, err := regularPreservationMember(preservationSnapshotPath)
	if err != nil {
		return PreservationMetadataV1{}, rehearsal, err
	}
	if !snapshotPresent {
		if terminalPreservationState(metadata.State) {
			return metadata, rehearsal, nil
		}
		return PreservationMetadataV1{}, rehearsal, ErrPreservationAmbiguous
	}
	snapshot, err := os.Open(preservationSnapshotPath) // #nosec G304 -- fixed package-owned preservation path.
	if err != nil {
		return PreservationMetadataV1{}, rehearsal, err
	}
	rehearsal, rehearsalErr := dbbackup.Rehearse(ctx, snapshot)
	closeErr := snapshot.Close()
	if rehearsalErr != nil || closeErr != nil || !rehearsal.Possible || rehearsal.ManifestStatus != "VERIFIED" || rehearsal.Manifest == nil {
		return PreservationMetadataV1{}, rehearsal, errors.Join(ErrPreservationUnavailable, rehearsalErr, closeErr)
	}
	if rehearsal.BackupDigest != metadata.ArtifactDigest || rehearsal.BackupBytes != metadata.ArtifactBytes ||
		rehearsal.Revision != metadata.RehearsalRevision || rehearsal.Manifest.BackupID != metadata.BackupID ||
		rehearsal.Manifest.AppVersion != metadata.AppVersion || rehearsal.Manifest.CoreSchema != metadata.CoreSchema ||
		rehearsal.Manifest.DeploymentProfile != metadata.DeploymentProfile ||
		rehearsal.Manifest.DeploymentRevision != metadata.DeploymentRevision {
		return PreservationMetadataV1{}, rehearsal, ErrPreservationChanged
	}
	return metadata, rehearsal, nil
}

func (metadata PreservationMetadataV1) Validate() error {
	if metadata.Schema != PreservationSchemaV1 ||
		(metadata.State != PreservationStatePrepared && metadata.State != PreservationStateRestoring &&
			metadata.State != PreservationStateRestored && metadata.State != PreservationStateBackupDone &&
			metadata.State != PreservationStateBackupFail) ||
		metadata.ArtifactPath != preservationSnapshotPath || !validPreservationDigest(metadata.Identity) ||
		metadata.Identity != preservationIdentity(metadata) || !validPreservationDigest(metadata.ArtifactDigest) ||
		!validPreservationDigest(metadata.BackupID) || !validPreservationDigest(metadata.RehearsalRevision) ||
		metadata.ArtifactBytes <= 0 || metadata.ArtifactBytes > dbbackup.MaxRestoreBytes || metadata.AppVersion == "" ||
		metadata.CoreSchema == "" || metadata.PreparedAt <= 0 {
		return ErrPreservationAmbiguous
	}
	if (metadata.State == PreservationStatePrepared || metadata.State == PreservationStateRestoring) &&
		(metadata.RestoredAt != 0 || metadata.RestoreOperationID != "" || metadata.RestoreOperationRev != 0 ||
			metadata.BackupCompletedAt != 0 || metadata.BackupFailedAt != 0) {
		return ErrPreservationAmbiguous
	}
	if metadata.State == PreservationStateRestored && (metadata.RestoredAt < metadata.PreparedAt ||
		!strings.HasPrefix(metadata.RestoreOperationID, "data-operation:") || metadata.RestoreOperationRev == 0 ||
		metadata.BackupCompletedAt != 0 || metadata.BackupFailedAt != 0) {
		return ErrPreservationAmbiguous
	}
	if metadata.State == PreservationStateBackupDone && (metadata.BackupCompletedAt < metadata.PreparedAt ||
		metadata.BackupFailedAt != 0 || metadata.RestoredAt != 0 || metadata.RestoreOperationID != "" || metadata.RestoreOperationRev != 0) {
		return ErrPreservationAmbiguous
	}
	if metadata.State == PreservationStateBackupFail && (metadata.BackupFailedAt < metadata.PreparedAt ||
		metadata.BackupCompletedAt != 0 || metadata.RestoredAt != 0 || metadata.RestoreOperationID != "" || metadata.RestoreOperationRev != 0) {
		return ErrPreservationAmbiguous
	}
	return nil
}

// RestoreSysupgradePreservationIfNeeded is called by the OpenWrt readiness
// wrapper before the panel starts. A normal fresh install has neither file;
// restoration is attempted only when the live DB is absent and the complete
// preservation pair is present.
func RestoreSysupgradePreservationIfNeeded(ctx context.Context, environment PreservationEnvironment) error {
	manager := datalifecycle.NewManager()
	// This path is startup recovery, before the resource-pressure observer has
	// started. Rehearsal rechecks current restore space and compatibility, so
	// admit only the one restore class needed by the existing lifecycle owner.
	manager.Admit = func(class string) bool { return class == "heavy_mutation" }
	return restoreSysupgradePreservationIfNeeded(ctx, environment, manager)
}

func restoreSysupgradePreservationIfNeeded(ctx context.Context, environment PreservationEnvironment, manager *datalifecycle.Manager) error {
	if ctx == nil || environment.validate() != nil || manager == nil {
		return ErrPreservationUnavailable
	}
	databasePath := configstorage.GetDBPath()
	databasePresent := false
	if info, err := os.Lstat(databasePath); err == nil {
		if !info.Mode().IsRegular() {
			return ErrPreservationAmbiguous
		}
		databasePresent = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := cleanupPreservationPartials(environment); err != nil {
		return err
	}
	metadataPresent, err := regularPreservationMember(preservationMetadataPath)
	if err != nil {
		return err
	}
	snapshotPresent, err := regularPreservationMember(preservationSnapshotPath)
	if err != nil {
		return err
	}
	if !metadataPresent && !snapshotPresent {
		return nil
	}
	if !metadataPresent {
		if databasePresent && snapshotPresent {
			return cleanupUnboundPreservationSnapshot(environment)
		}
		return ErrPreservationAmbiguous
	}
	metadata, err := loadPreservationMetadata()
	if err != nil {
		return err
	}
	if terminalPreservationState(metadata.State) {
		if !databasePresent {
			return ErrPreservationAmbiguous
		}
		return cleanupTerminalPreservation(environment)
	}
	if !snapshotPresent {
		return ErrPreservationAmbiguous
	}
	if databasePresent {
		switch metadata.State {
		case PreservationStatePrepared:
			return terminalizeBackupPreservation(ctx, environment, false)
		case PreservationStateRestoring:
			if _, _, err := LoadSysupgradePreservation(ctx); err != nil {
				return err
			}
			if err := cleanupFreshRestoreBootstrap(databasePath); err != nil {
				return err
			}
		default:
			return ErrPreservationAmbiguous
		}
	}
	metadata, rehearsal, err := LoadSysupgradePreservation(ctx)
	if err != nil {
		return err
	}
	if metadata.State == PreservationStateRestored {
		return ErrPreservationAmbiguous
	}
	if metadata.State == PreservationStatePrepared {
		metadata.State = PreservationStateRestoring
		metadata.Identity = preservationIdentity(metadata)
		if err := writePreservationMetadata(metadata, environment.SyncDirectory); err != nil {
			return err
		}
	} else if metadata.State != PreservationStateRestoring {
		return ErrPreservationAmbiguous
	}
	if err := restorestate.Recover(databasePath); err != nil {
		return err
	}
	if err := dbsqlite.Init(databasePath); err != nil {
		return err
	}
	cleanupBootstrap := true
	defer func() {
		if cleanupBootstrap {
			_ = cleanupFreshRestoreBootstrap(databasePath)
		}
	}()
	if err := migration.EnsureCurrentSchemaJournal(dbsqlite.DB(), true); err != nil {
		return err
	}
	if reconcileErr := manager.ReconcileStartup(ctx); reconcileErr != nil {
		return reconcileErr
	}
	snapshot, err := os.Open(preservationSnapshotPath) // #nosec G304 -- fixed package-owned preservation path.
	if err != nil {
		return err
	}
	idempotency := "openwrt-sysupgrade-" + metadata.ArtifactDigest[:32]
	operation, _, restoreErr := manager.ExecuteRestore(ctx, datalifecycle.RestoreRequest{
		ExpectedRehearsalRevision: rehearsal.Revision, IdempotencyKey: idempotency,
		Confirmation: datalifecycle.RestoreConfirmation(rehearsal.Revision), Acknowledged: true, Source: snapshot,
	})
	closeErr := snapshot.Close()
	if restoreErr != nil || closeErr != nil || operation.State != "APPLIED" {
		return errors.Join(restoreErr, closeErr, ErrPreservationUnavailable)
	}
	if err := dbsqlite.RemoveInitialAdminPasswordFile(); err != nil {
		return err
	}
	if err := dbsqlite.Close(); err != nil {
		return err
	}
	metadata.State, metadata.RestoredAt = PreservationStateRestored, environment.Now().UTC().Unix()
	metadata.RestoreOperationID, metadata.RestoreOperationRev = operation.OperationID, operation.Revision
	metadata.Identity = preservationIdentity(metadata)
	if err := writePreservationMetadata(metadata, environment.SyncDirectory); err != nil {
		return err
	}
	cleanupBootstrap = false
	return cleanupTerminalPreservation(environment)
}

// FinalizeSysupgradePreservationBackup retires the live-root bulk snapshot
// after native sysupgrade has finished copying it into its archive. The small
// metadata tombstone remains as the durable generation fence.
func FinalizeSysupgradePreservationBackup(ctx context.Context, environment PreservationEnvironment, successful bool) error {
	if ctx == nil || environment.validate() != nil {
		return ErrPreservationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	databasePresent, err := regularPreservationMember(configstorage.GetDBPath())
	if err != nil || !databasePresent {
		return errors.Join(ErrPreservationAmbiguous, err)
	}
	return terminalizeBackupPreservation(ctx, environment, successful)
}

func reconcileBeforePreservationPreparation(ctx context.Context, environment PreservationEnvironment) error {
	if err := cleanupPreservationPartials(environment); err != nil {
		return err
	}
	metadataPresent, err := regularPreservationMember(preservationMetadataPath)
	if err != nil {
		return err
	}
	snapshotPresent, err := regularPreservationMember(preservationSnapshotPath)
	if err != nil {
		return err
	}
	if !metadataPresent && !snapshotPresent {
		return nil
	}
	if !metadataPresent {
		databasePresent, databaseErr := regularPreservationMember(configstorage.GetDBPath())
		if databaseErr != nil || !databasePresent || !snapshotPresent {
			return errors.Join(ErrPreservationAmbiguous, databaseErr)
		}
		return cleanupUnboundPreservationSnapshot(environment)
	}
	databasePresent, err := regularPreservationMember(configstorage.GetDBPath())
	if err != nil || !databasePresent {
		return errors.Join(ErrPreservationAmbiguous, err)
	}
	metadata, err := loadPreservationMetadata()
	if err != nil {
		return err
	}
	switch metadata.State {
	case PreservationStatePrepared:
		if !snapshotPresent {
			return ErrPreservationAmbiguous
		}
		return terminalizeBackupPreservation(ctx, environment, false)
	case PreservationStateBackupDone, PreservationStateBackupFail, PreservationStateRestored:
		return cleanupTerminalPreservation(environment)
	default:
		return ErrPreservationAmbiguous
	}
}

func terminalizeBackupPreservation(ctx context.Context, environment PreservationEnvironment, successful bool) error {
	metadata, err := loadPreservationMetadata()
	if err != nil {
		return err
	}
	switch metadata.State {
	case PreservationStatePrepared:
		if _, _, err := LoadSysupgradePreservation(ctx); err != nil {
			return err
		}
		if successful {
			metadata.State = PreservationStateBackupDone
			metadata.BackupCompletedAt = environment.Now().UTC().Unix()
		} else {
			metadata.State = PreservationStateBackupFail
			metadata.BackupFailedAt = environment.Now().UTC().Unix()
		}
		metadata.Identity = preservationIdentity(metadata)
		if err := writePreservationMetadata(metadata, environment.SyncDirectory); err != nil {
			return err
		}
	case PreservationStateBackupDone, PreservationStateBackupFail, PreservationStateRestored:
		// A terminal tombstone plus a bulk snapshot is explicit cleanup debt.
	default:
		return ErrPreservationAmbiguous
	}
	return cleanupTerminalPreservation(environment)
}

func cleanupTerminalPreservation(environment PreservationEnvironment) error {
	if environment.RemoveFile == nil || environment.SyncDirectory == nil {
		return ErrPreservationUnavailable
	}
	present, err := regularPreservationMember(preservationSnapshotPath)
	if err != nil {
		return err
	}
	if present {
		if err := environment.RemoveFile(preservationSnapshotPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	// Sync even when a previous attempt already unlinked the file: terminal
	// metadata is the durable cleanup-debt marker until this barrier succeeds.
	return environment.SyncDirectory(preservationRootPath)
}

func cleanupUnboundPreservationSnapshot(environment PreservationEnvironment) error {
	if err := environment.RemoveFile(preservationSnapshotPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return environment.SyncDirectory(preservationRootPath)
}

func cleanupPreservationPartials(environment PreservationEnvironment) error {
	removed := false
	for _, name := range []string{preservationSnapshotPath + ".partial", preservationMetadataPath + ".partial"} {
		present, err := regularPreservationMember(name)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if err := environment.RemoveFile(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed = true
	}
	if removed {
		return environment.SyncDirectory(preservationRootPath)
	}
	return nil
}

func terminalPreservationState(state string) bool {
	return state == PreservationStateRestored || state == PreservationStateBackupDone || state == PreservationStateBackupFail
}

func preservationRequiredBytes(artifactBytes int64) (uint64, error) {
	if artifactBytes <= 0 || artifactBytes > dbbackup.MaxRestoreBytes {
		return 0, ErrPreservationAmbiguous
	}
	return uint64(artifactBytes) + uint64(MaxPreservationMetadata), nil
}

func installPreservationSnapshot(ctx context.Context, sourcePath string, rehearsal dbbackup.RestoreRehearsal, syncDirectory func(string) error) error {
	partial := preservationSnapshotPath + ".partial"
	if err := removeRegularOrAbsent(partial); err != nil {
		return err
	}
	source, err := os.Open(sourcePath) // #nosec G304 -- generated backup owner path.
	if err != nil {
		return err
	}
	destination, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = source.Close()
		return err
	}
	hash := sha256.New()
	written, copyErr := copyPreservation(ctx, io.MultiWriter(destination, hash), io.LimitReader(source, dbbackup.MaxRestoreBytes+1))
	syncErr, destinationCloseErr, sourceCloseErr := destination.Sync(), destination.Close(), source.Close()
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if copyErr != nil || syncErr != nil || destinationCloseErr != nil || sourceCloseErr != nil ||
		written != rehearsal.BackupBytes || written <= 0 || written > dbbackup.MaxRestoreBytes || actualDigest != rehearsal.BackupDigest {
		_ = os.Remove(partial)
		return errors.Join(copyErr, syncErr, destinationCloseErr, sourceCloseErr, ErrPreservationChanged)
	}
	if err := replacePreservationFile(partial, preservationSnapshotPath); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return syncDirectory(preservationRootPath)
}

func writePreservationMetadata(metadata PreservationMetadataV1, syncDirectory func(string) error) error {
	if metadata.Validate() != nil || syncDirectory == nil {
		return ErrPreservationAmbiguous
	}
	data, err := json.Marshal(metadata)
	if err != nil || len(data)+1 > MaxPreservationMetadata {
		return ErrPreservationAmbiguous
	}
	data = append(data, '\n')
	partial := preservationMetadataPath + ".partial"
	if err := removeRegularOrAbsent(partial); err != nil {
		return err
	}
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(partial)
		return errors.Join(writeErr, syncErr, closeErr)
	}
	if err := replacePreservationFile(partial, preservationMetadataPath); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return syncDirectory(preservationRootPath)
}

func ensurePreservationRoot(syncDirectory func(string) error) error {
	if syncDirectory == nil {
		return ErrPreservationUnavailable
	}
	if filepath.Clean(preservationRootPath) != preservationRootPath ||
		filepath.Dir(preservationRootPath) != DefaultDatabaseFolder ||
		filepath.Dir(preservationSnapshotPath) != preservationRootPath || filepath.Dir(preservationMetadataPath) != preservationRootPath {
		return ErrPreservationAmbiguous
	}
	if info, err := os.Lstat(preservationRootPath); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrPreservationAmbiguous
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(preservationRootPath, 0o700); err != nil {
			return err
		}
		if err := syncDirectory(DefaultDatabaseFolder); err != nil {
			return err
		}
	} else {
		return err
	}
	for _, name := range []string{preservationSnapshotPath, preservationMetadataPath} {
		if info, err := os.Lstat(name); err == nil && !info.Mode().IsRegular() {
			return ErrPreservationAmbiguous
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func preservationIdentity(metadata PreservationMetadataV1) string {
	metadata.Identity = ""
	data, _ := json.Marshal(metadata)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadPreservationMetadata() (PreservationMetadataV1, error) {
	data, err := readBoundedRegularFile(preservationMetadataPath, MaxPreservationMetadata)
	if err != nil {
		return PreservationMetadataV1{}, err
	}
	metadata := PreservationMetadataV1{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil || decoder.Decode(&struct{}{}) != io.EOF || metadata.Validate() != nil {
		return PreservationMetadataV1{}, ErrPreservationAmbiguous
	}
	return metadata, nil
}

func readBoundedRegularFile(name string, limit int64) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, ErrPreservationAmbiguous
	}
	return os.ReadFile(name) // #nosec G304 -- fixed package-owned preservation path.
}

func regularPreservationMember(name string) (bool, error) {
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrPreservationAmbiguous
	}
	return true, nil
}

func removeRegularOrAbsent(name string) error {
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrPreservationAmbiguous
	}
	return os.Remove(name)
}

func replacePreservationFile(source, destination string) error {
	if info, err := os.Lstat(destination); err == nil && !info.Mode().IsRegular() {
		return ErrPreservationAmbiguous
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

func copyPreservation(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil || written != read {
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

func cleanupFreshRestoreBootstrap(databasePath string) error {
	closeErr := dbsqlite.Close()
	recoverErr := restorestate.Recover(databasePath)
	if recoverErr != nil {
		return errors.Join(closeErr, recoverErr)
	}
	var removeErr error
	for _, name := range []string{databasePath, databasePath + "-wal", databasePath + "-shm", databasePath + "-journal"} {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			removeErr = errors.Join(removeErr, err)
		}
	}
	removeErr = errors.Join(removeErr, dbsqlite.RemoveInitialAdminPasswordFile())
	return errors.Join(closeErr, removeErr)
}

func validPreservationDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
