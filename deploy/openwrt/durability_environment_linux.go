//go:build linux

package openwrt

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

func RefreshDatabaseDurabilityProof() (DatabaseDurabilityProofV3, error) {
	if os.Geteuid() != 0 {
		return DatabaseDurabilityProofV3{}, errors.New("OpenWrt durability proof writer requires root")
	}
	proof, err := createDatabaseDurabilityProof(DefaultDatabaseFolder, productionDurabilityEnvironment())
	if err != nil {
		return DatabaseDurabilityProofV3{}, err
	}
	data, err := json.Marshal(proof)
	if err != nil || len(data)+1 > MaxDurabilityProofBytes {
		return DatabaseDurabilityProofV3{}, ErrUnprovenDurableState
	}
	if err := writeDurabilityProof(append(data, '\n')); err != nil {
		return DatabaseDurabilityProofV3{}, err
	}
	return proof, nil
}

func LoadDatabaseDurabilityProof() (DatabaseDurabilityProofV3, error) {
	info, err := os.Lstat(DefaultDurabilityProofPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > MaxDurabilityProofBytes || info.Mode().Perm() != 0o444 {
		return DatabaseDurabilityProofV3{}, errors.Join(ErrUnprovenDurableState, err)
	}
	before, ok := info.Sys().(*syscall.Stat_t)
	if !ok || before.Uid != 0 || before.Gid != 0 {
		return DatabaseDurabilityProofV3{}, ErrUnprovenDurableState
	}
	file, err := os.Open(DefaultDurabilityProofPath) // #nosec G304 -- fixed package-owned proof.
	if err != nil {
		return DatabaseDurabilityProofV3{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxDurabilityProofBytes+1))
	afterInfo, statErr := file.Stat()
	closeErr := file.Close()
	var after *syscall.Stat_t
	afterOK := false
	if statErr == nil && afterInfo != nil {
		after, afterOK = afterInfo.Sys().(*syscall.Stat_t)
	}
	if readErr != nil || statErr != nil || closeErr != nil || len(data) == 0 || len(data) > MaxDurabilityProofBytes || !afterOK ||
		before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim {
		return DatabaseDurabilityProofV3{}, errors.Join(ErrUnprovenDurableState, readErr, statErr, closeErr)
	}
	var proof DatabaseDurabilityProofV3
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proof); err != nil || decoder.Decode(&struct{}{}) != io.EOF || proof.Validate() != nil {
		return DatabaseDurabilityProofV3{}, ErrUnprovenDurableState
	}
	return proof, nil
}

func InspectDatabaseDurableState(required uint64) (DurableStateEvidence, error) {
	if configstorage.GetDBFolderPath() != DefaultDatabaseFolder {
		return DurableStateEvidence{}, ErrUnprovenDurableState
	}
	proof, err := LoadDatabaseDurabilityProof()
	if err != nil {
		return DurableStateEvidence{}, err
	}
	evidence, err := recheckDatabaseDurabilityProof(proof, required, productionDurabilityEnvironment(), mountevidence.AvailableBytes)
	if err != nil {
		return DurableStateEvidence{}, err
	}
	if err := syncPreservationDirectory(DefaultDatabaseFolder); err != nil {
		return DurableStateEvidence{}, err
	}
	databasePath := configstorage.GetDBPath()
	if info, statErr := os.Lstat(databasePath); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return DurableStateEvidence{}, ErrUnprovenDurableState
		}
		file, openErr := os.OpenFile(databasePath, os.O_RDWR, 0) // #nosec G304 -- fixed profile-derived database path.
		if openErr != nil {
			return DurableStateEvidence{}, openErr
		}
		syncErr, closeErr := file.Sync(), file.Close()
		if syncErr != nil || closeErr != nil {
			return DurableStateEvidence{}, errors.Join(syncErr, closeErr)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return DurableStateEvidence{}, statErr
	}
	return evidence, nil
}

func productionDurabilityEnvironment() durabilityEnvironment {
	return durabilityEnvironment{
		Observe: mountevidence.Observe, ObserveLabel: mountevidence.ObserveOverlayLabel,
		PersistenceAuthority: selectedPersistenceAuthority(),
	}
}

func writeDurabilityProof(data []byte) error {
	directory := filepath.Dir(DefaultDurabilityProofPath)
	parent, err := os.Lstat(directory)
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0o022 != 0 {
		return errors.Join(ErrUnprovenDurableState, err)
	}
	parentStat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || parentStat.Uid != 0 || parentStat.Gid != 0 {
		return ErrUnprovenDurableState
	}
	temporary := DefaultDurabilityProofPath + ".incoming"
	if info, statErr := os.Lstat(temporary); statErr == nil {
		if !info.Mode().IsRegular() {
			return ErrUnprovenDurableState
		}
		if err := os.Remove(temporary); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o444)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return errors.Join(writeErr, syncErr, closeErr)
	}
	if err := os.Chown(temporary, 0, 0); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, DefaultDurabilityProofPath); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncPreservationDirectory(directory)
}
