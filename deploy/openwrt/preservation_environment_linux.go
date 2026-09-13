//go:build linux

package openwrt

import (
	"errors"
	"os"
	"time"
)

// ProductionPreservationEnvironment derives only the fixed destination facts
// needed by the OpenWrt deployment owner.
func ProductionPreservationEnvironment() PreservationEnvironment {
	return PreservationEnvironment{InspectDurableState: func(databaseFolder string, required uint64) (DurableStateEvidence, error) {
		if databaseFolder != DefaultDatabaseFolder {
			return DurableStateEvidence{}, ErrUnprovenDurableState
		}
		return InspectDatabaseDurableState(required)
	}, SyncDirectory: syncPreservationDirectory, RemoveFile: os.Remove, Now: time.Now}
}

func syncPreservationDirectory(name string) error {
	directory, err := os.Open(name) // #nosec G304 -- fixed package-owned directory.
	if err != nil {
		return err
	}
	err = directory.Sync()
	return errors.Join(err, directory.Close())
}
