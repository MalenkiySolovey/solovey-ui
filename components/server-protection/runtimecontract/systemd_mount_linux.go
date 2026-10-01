//go:build linux

package runtimecontract

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

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
// filesystem allocation/performance options are not durable backing identity.
// Preserve the generic superblock access/security policy separately; the complete
// current proof (including all options) is compared exactly on every recheck.
// This adapter is selected by authenticated Systemd deployment authority only.
func sameSystemdRuntimeBacking(installed, current RuntimeMountProofV1) bool {
	if installed.Validate() != nil || current.Validate() != nil ||
		installed.Policy != RuntimeMountPersistent || current.Policy != RuntimeMountPersistent || installed.Root != current.Root {
		return false
	}
	a, b := installed.Mount, current.Mount
	return a.Target == b.Target && a.ResolvedTarget == b.ResolvedTarget && a.Device == b.Device &&
		a.Filesystem == b.Filesystem && a.FilesystemMagic == b.FilesystemMagic && a.Source == b.Source &&
		sameSystemdSuperblockPolicy(a, b) && backingPath(a) == backingPath(b)
}

// Linux mountinfo combines generic superblock flags, LSM labels, and arbitrary
// filesystem show_options output. Only the first two describe our cross-boot
// access policy. Filesystem tuning can be defaulted or omitted on a later boot
// without changing the device or directory being authorized. This projection is
// Systemd-local: raw evidence, procd/Docker authority and live Recheck stay exact.
func sameSystemdSuperblockPolicy(a, b mountevidence.Fact) bool {
	// Check superblock rw explicitly as well as the per-mount and statfs flags
	// required by proof.Validate. Conflicting or missing evidence fails closed.
	if !a.HasOption("rw") || !b.HasOption("rw") || a.HasOption("ro") || b.HasOption("ro") {
		return false
	}
	policy := func(options []string) []string {
		var result []string
		for _, option := range options {
			key, _, _ := strings.Cut(option, "=")
			switch key {
			case "sync", "dirsync", "mand", "lazytime", "acl", "noacl", "user_xattr", "nouser_xattr",
				"context", "fscontext", "defcontext", "rootcontext", "seclabel",
				"smackfsdef", "smackfsfloor", "smackfshat", "smackfsroot", "smackfstransmute":
				result = append(result, option)
			}
		}
		return result
	}
	return slices.Equal(policy(a.SuperOptions), policy(b.SuperOptions))
}

func backingPath(fact mountevidence.Fact) string {
	relative, err := filepath.Rel(fact.MountPoint, fact.ResolvedTarget)
	if err != nil {
		return ""
	}
	return filepath.Join(fact.Root, relative)
}
