package openwrt

import (
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
)

const (
	StorageSelectionPath     = "/etc/solovey-ui/deployment-storage.json"
	StorageSelectionSchema   = "solovey-ui/deployment-storage/v1"
	MaxStorageSelectionBytes = 4096
)

// StorageSelection is an explicit deployment input, never an OS classifier.
// The zero value retains the accepted package storage contract. A selected
// direct mount must already be mounted by its platform owner.
type StorageSelection struct {
	Schema      string `json:"schema"`
	DurableRoot string `json:"durableRoot"`
	ProofClass  string `json:"proofClass"`
	MountPoint  string `json:"mountPoint"`
}

func (s StorageSelection) IsDefault() bool { return s == (StorageSelection{}) }

func (s StorageSelection) Validate() error {
	if s.IsDefault() {
		return nil
	}
	if s.Schema != StorageSelectionSchema || s.ProofClass != DirectPersistentMount ||
		!canonicalAbsolute(s.DurableRoot) || !canonicalAbsolute(s.MountPoint) ||
		s.DurableRoot == s.MountPoint || !pathContains(s.MountPoint, s.DurableRoot) ||
		isVolatileDurableRoot(s.DurableRoot) || strings.ContainsAny(s.DurableRoot+s.MountPoint, " '\"\\$`;|&<>()*?[]{}!") {
		return ErrUnprovenDurableState
	}
	p := DefaultProfile()
	p.DatabaseFolder = s.DatabaseFolder()
	return p.Validate()
}

func (s StorageSelection) Root() string {
	if s.IsDefault() {
		return path.Dir(DefaultDatabaseFolder)
	}
	return s.DurableRoot
}

func (s StorageSelection) DatabaseFolder() string { return path.Join(s.Root(), "db") }
func (s StorageSelection) InstanceIDPath() string { return path.Join(s.Root(), "openwrt-instance-id") }
func (s StorageSelection) Profile() ProfileV1 {
	p := DefaultProfile()
	p.DatabaseFolder = s.DatabaseFolder()
	return p
}

func parseStorageSelection(data []byte) (StorageSelection, error) {
	var s StorageSelection
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if len(data) == 0 || len(data) > MaxStorageSelectionBytes || d.Decode(&s) != nil || d.Decode(&struct{}{}) != io.EOF || s.IsDefault() || s.Validate() != nil {
		return StorageSelection{}, errors.New("deployment storage selection is invalid")
	}
	return s, nil
}
