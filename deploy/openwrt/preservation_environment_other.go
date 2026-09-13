//go:build !linux

package openwrt

import (
	"errors"
	"os"
	"time"
)

func ProductionPreservationEnvironment() PreservationEnvironment {
	unavailable := func(string, uint64) (DurableStateEvidence, error) {
		return DurableStateEvidence{}, errors.New("OpenWrt preservation environment is supported only on Linux")
	}
	return PreservationEnvironment{InspectDurableState: unavailable, SyncDirectory: func(string) error {
		return errors.New("OpenWrt directory durability is supported only on Linux")
	}, RemoveFile: os.Remove, Now: time.Now}
}
