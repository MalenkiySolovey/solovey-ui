//go:build linux

package runtimecontract

import (
	"errors"
	"path/filepath"
	"slices"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

func bindInstalledRuntimeMount(authority RuntimeRootAuthority) (RuntimeRootAuthority, error) {
	if authority.backend != DeploymentBackendSystemd {
		return authority, nil
	}
	local, err := bindSystemdRuntimeMountProof(authority.mount)
	if err != nil {
		return RuntimeRootAuthority{}, err
	}
	authority.localMount = &local
	return authority, nil
}

func bindSystemdRuntimeMountProof(installed RuntimeMountProofV1) (RuntimeMountProofV1, error) {
	current, err := ObserveRuntimeMount(installed.Root, RuntimeMountPersistent)
	if err != nil {
		return RuntimeMountProofV1{}, err
	}
	if !sameSystemdRuntimeBacking(installed, current) {
		return RuntimeMountProofV1{}, errors.New("systemd runtime mount backing differs from installed authority")
	}
	return current, nil
}

// Bind mounts created by ReadWritePaths have namespace-local mount/parent IDs,
// mount points and roots. Compare their resolved path within the same backing
// filesystem, not those namespace coordinates. Both proofs must be writable;
// the complete current proof is retained and compared exactly on every recheck.
// This adapter is selected by authenticated Systemd deployment authority only.
func sameSystemdRuntimeBacking(installed, current RuntimeMountProofV1) bool {
	if installed.Validate() != nil || current.Validate() != nil ||
		installed.Policy != RuntimeMountPersistent || current.Policy != RuntimeMountPersistent || installed.Root != current.Root {
		return false
	}
	a, b := installed.Mount, current.Mount
	return a.Target == b.Target && a.ResolvedTarget == b.ResolvedTarget && a.Device == b.Device &&
		a.Filesystem == b.Filesystem && a.FilesystemMagic == b.FilesystemMagic && a.Source == b.Source &&
		slices.Equal(a.SuperOptions, b.SuperOptions) && backingPath(a) == backingPath(b)
}

func backingPath(fact mountevidence.Fact) string {
	relative, err := filepath.Rel(fact.MountPoint, fact.ResolvedTarget)
	if err != nil {
		return ""
	}
	return filepath.Join(fact.Root, relative)
}
