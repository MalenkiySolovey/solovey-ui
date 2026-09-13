//go:build linux

package deploymentidentity

import (
	"errors"
	"fmt"
	"os"
)

var ErrExpectedApplicationOwnerUnavailable = errors.New("expected application owner is unavailable")

type installedExpectedOwnerAdapter struct {
	backend InstalledApplicationBackend
	path    string
	load    func() (ExpectedApplicationOwnerV1, error)
}

var installedExpectedOwnerAdapters []installedExpectedOwnerAdapter

func registerInstalledExpectedOwnerAdapter(adapter installedExpectedOwnerAdapter) {
	installedExpectedOwnerAdapters = append(installedExpectedOwnerAdapters, adapter)
}

// LoadExpectedApplicationOwner selects exactly one installed backend proof and
// returns only its semantic projection. Existing-but-invalid and ambiguous
// proofs fail closed.
func LoadExpectedApplicationOwner() (ExpectedApplicationOwnerV1, error) {
	projection, err := LoadInstalledApplicationOwnerProjection()
	if err != nil {
		return ExpectedApplicationOwnerV1{}, err
	}
	return projection.Owner, nil
}

// LoadInstalledApplicationOwnerProjection selects the installed backend once
// and retains both its neutral owner fact and concrete backend identity.
func LoadInstalledApplicationOwnerProjection() (InstalledApplicationOwnerProjection, error) {
	return loadInstalledApplicationOwnerProjection(installedExpectedOwnerAdapters)
}

func loadInstalledApplicationOwnerProjection(adapters []installedExpectedOwnerAdapter) (InstalledApplicationOwnerProjection, error) {
	var selected InstalledApplicationOwnerProjection
	matches := 0
	for _, adapter := range adapters {
		if _, err := os.Lstat(adapter.path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return InstalledApplicationOwnerProjection{}, fmt.Errorf("inspect %s application owner proof: %w", adapter.backend, err)
		}
		value, err := adapter.load()
		if err != nil {
			return InstalledApplicationOwnerProjection{}, fmt.Errorf("load %s application owner proof: %w", adapter.backend, err)
		}
		selected, err = NewInstalledApplicationOwnerProjection(adapter.backend, value)
		if err != nil {
			return InstalledApplicationOwnerProjection{}, err
		}
		matches++
	}
	if matches == 0 {
		return InstalledApplicationOwnerProjection{}, ErrExpectedApplicationOwnerUnavailable
	}
	if matches != 1 {
		return InstalledApplicationOwnerProjection{}, errors.New("installed application owner proof is ambiguous")
	}
	return selected, nil
}

// Recheck proves that every fact which selected this projection still names
// the same sole backend and exact owner generation.
func (p InstalledApplicationOwnerProjection) Recheck() error {
	return p.recheck(installedExpectedOwnerAdapters)
}

func (p InstalledApplicationOwnerProjection) recheck(adapters []installedExpectedOwnerAdapter) error {
	if err := p.Validate(); err != nil {
		return err
	}
	current, err := loadInstalledApplicationOwnerProjection(adapters)
	if err != nil {
		return err
	}
	if current != p {
		return errors.New("installed application owner projection changed")
	}
	return nil
}

// LoadExpectedProcdApplicationOwner requires the procd backend proof and
// rejects a simultaneously installed Systemd proof. Explicit provider
// selection must not weaken this boundary.
func LoadExpectedProcdApplicationOwner() (ExpectedApplicationOwnerV1, error) {
	contract, err := LoadBoundProcdInstalled()
	if err != nil {
		return ExpectedApplicationOwnerV1{}, err
	}
	return ExpectedProcdApplicationOwner(contract)
}

// LoadBoundProcdInstalled returns the backend proof only when it is the sole
// installed deployment proof.
func LoadBoundProcdInstalled() (ApplicationOwnerContractProcdV1, error) {
	return LoadBoundProcdFromPaths(InstalledContractPath, ProcdInstalledContractPath)
}

// LoadBoundProcdFromPaths applies the production exclusivity and contract
// loading rules to explicit absolute fact paths. The fixed-path production
// loader above is the authority; this form exists for root-owned composition
// fixtures and offline verification without changing selection semantics.
func LoadBoundProcdFromPaths(systemdPath, procdPath string) (ApplicationOwnerContractProcdV1, error) {
	if !canonicalAbsolute(systemdPath) || !canonicalAbsolute(procdPath) || systemdPath == procdPath {
		return ApplicationOwnerContractProcdV1{}, errors.New("application owner proof paths are invalid")
	}
	if _, err := os.Lstat(systemdPath); err == nil {
		return ApplicationOwnerContractProcdV1{}, errors.New("procd application owner proof conflicts with an installed Systemd proof")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ApplicationOwnerContractProcdV1{}, fmt.Errorf("inspect Systemd application owner proof: %w", err)
	}
	if _, err := os.Lstat(procdPath); errors.Is(err, os.ErrNotExist) {
		return ApplicationOwnerContractProcdV1{}, ErrExpectedApplicationOwnerUnavailable
	} else if err != nil {
		return ApplicationOwnerContractProcdV1{}, fmt.Errorf("inspect procd application owner proof: %w", err)
	}
	return LoadProcdFromPath(procdPath)
}
