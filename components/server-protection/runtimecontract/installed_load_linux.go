//go:build linux

package runtimecontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

var ErrInstalledRuntimeRootUnavailable = errors.New("installed Server Protection runtime root is unavailable")

func LoadInstalledRuntimeRoot() (InstalledRuntimeRootV1, error) {
	projection, err := deploymentidentity.LoadInstalledApplicationOwnerProjection()
	if err != nil {
		return InstalledRuntimeRootV1{}, err
	}
	value, err := loadInstalledRuntimeRoot()
	if err != nil {
		return InstalledRuntimeRootV1{}, err
	}
	if err := ValidateInstalledRuntimeRootBinding(value, projection.Owner); err != nil {
		return InstalledRuntimeRootV1{}, err
	}
	return value, nil
}

func loadInstalledRuntimeRoot() (InstalledRuntimeRootV1, error) {
	info, err := os.Lstat(InstalledRuntimeRootPath)
	if errors.Is(err, os.ErrNotExist) {
		return InstalledRuntimeRootV1{}, ErrInstalledRuntimeRootUnavailable
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 64<<10 {
		return InstalledRuntimeRootV1{}, errors.New("installed Server Protection runtime root file is unsafe")
	}
	parent, err := os.Lstat(path.Dir(InstalledRuntimeRootPath))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0o022 != 0 {
		return InstalledRuntimeRootV1{}, errors.New("installed Server Protection runtime root parent is unsafe")
	}
	parentStat, parentOK := parent.Sys().(*syscall.Stat_t)
	before, fileOK := info.Sys().(*syscall.Stat_t)
	if !parentOK || parentStat.Uid != 0 || parentStat.Gid != 0 || !fileOK || before.Uid != 0 || before.Gid != 0 || info.Mode().Perm() != 0o444 {
		return InstalledRuntimeRootV1{}, errors.New("installed Server Protection runtime root ownership is unsafe")
	}
	file, err := os.Open(InstalledRuntimeRootPath)
	if err != nil {
		return InstalledRuntimeRootV1{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 64<<10+1))
	afterInfo, statErr := file.Stat()
	closeErr := file.Close()
	var after *syscall.Stat_t
	afterOK := false
	if statErr == nil && afterInfo != nil {
		after, afterOK = afterInfo.Sys().(*syscall.Stat_t)
	}
	if readErr != nil || statErr != nil || closeErr != nil || len(data) == 0 || len(data) > 64<<10 || !afterOK ||
		before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim {
		return InstalledRuntimeRootV1{}, errors.New("installed Server Protection runtime root changed while reading")
	}
	var value InstalledRuntimeRootV1
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return InstalledRuntimeRootV1{}, fmt.Errorf("decode installed Server Protection runtime root: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return InstalledRuntimeRootV1{}, errors.New("installed Server Protection runtime root contains multiple values")
	}
	return value, nil
}

func LoadInstalledRuntimeRootAuthority() (RuntimeRootAuthority, error) {
	projection, err := deploymentidentity.LoadInstalledApplicationOwnerProjection()
	if err != nil {
		return RuntimeRootAuthority{}, err
	}
	runtimeRoot, err := loadInstalledRuntimeRoot()
	if err != nil {
		return RuntimeRootAuthority{}, err
	}
	authority, err := InstalledRuntimeRootAuthority(runtimeRoot, projection)
	if err != nil {
		return RuntimeRootAuthority{}, err
	}
	if err := authority.Recheck(); err != nil {
		return RuntimeRootAuthority{}, err
	}
	return authority, nil
}

func ResolveRootAuthority(databaseFolder string) (RuntimeRootAuthority, error) {
	if os.Getenv("SUI_DEPLOYMENT_KIND") == "docker" {
		profileID := domain.ProfileID(strings.TrimSpace(os.Getenv("SOLOVEY_DEPLOYMENT_PROFILE")))
		profile, ok := domain.Lookup(profileID)
		if !ok || profile.Runtime != domain.RuntimeDocker {
			return RuntimeRootAuthority{}, errors.New("Docker Server Protection deployment profile is unavailable")
		}
		if os.Getenv("SUI_SERVER_PROTECTION_RUNTIME_ROOT") != DockerRuntimeRoot {
			return RuntimeRootAuthority{}, errors.New("Docker Server Protection runtime root injection is absent or non-canonical")
		}
		mount, err := ObserveRuntimeMount(DockerRuntimeRoot, RuntimeMountVolatile)
		if err != nil {
			return RuntimeRootAuthority{}, err
		}
		authority, err := dockerRuntimeRootAuthority(mount, string(profile.ID), profile.Revision)
		if err != nil {
			return RuntimeRootAuthority{}, err
		}
		if err := authority.Recheck(); err != nil {
			return RuntimeRootAuthority{}, err
		}
		return authority, nil
	}
	authority, err := resolveRootAuthority(databaseFolder, loadInstalledRuntimeRoot, deploymentidentity.LoadInstalledApplicationOwnerProjection)
	if err != nil {
		return RuntimeRootAuthority{}, err
	}
	if authority.Installed() {
		if err := authority.Recheck(); err != nil {
			return RuntimeRootAuthority{}, err
		}
	}
	return authority, nil
}

func recheckRuntimeRootAuthority(authority RuntimeRootAuthority) error {
	switch authority.backend {
	case DeploymentBackendSystemd, DeploymentBackendProcd:
		return recheckRetainedInstalledProjection(authority,
			deploymentidentity.LoadInstalledApplicationOwnerProjection, loadInstalledRuntimeRoot,
			func() error { return recheckRuntimeMount(authority.mount) })
	case DeploymentBackendDocker:
		profileID := domain.ProfileID(strings.TrimSpace(os.Getenv("SOLOVEY_DEPLOYMENT_PROFILE")))
		profile, ok := domain.Lookup(profileID)
		if os.Getenv("SUI_DEPLOYMENT_KIND") != "docker" || os.Getenv("SUI_SERVER_PROTECTION_RUNTIME_ROOT") != DockerRuntimeRoot ||
			!ok || profile.Runtime != domain.RuntimeDocker || authority.dockerProfileID != "" &&
			(string(profile.ID) != authority.dockerProfileID || profile.Revision != authority.dockerProfileRevision) {
			return errors.New("Docker Server Protection deployment projection changed")
		}
	default:
		return errors.New("installed Server Protection deployment backend is unavailable")
	}
	return recheckRuntimeMount(authority.mount)
}

func ResolveRoot(databaseFolder string) (string, error) {
	authority, err := ResolveRootAuthority(databaseFolder)
	return authority.Path(), err
}
