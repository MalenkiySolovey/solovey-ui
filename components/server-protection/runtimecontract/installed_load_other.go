//go:build !linux

package runtimecontract

import (
	"errors"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
)

var ErrInstalledRuntimeRootUnavailable = errors.New("installed Server Protection runtime root is unavailable")

func LoadInstalledRuntimeRoot() (InstalledRuntimeRootV1, error) {
	return InstalledRuntimeRootV1{}, ErrInstalledRuntimeRootUnavailable
}

func LoadInstalledRuntimeRootAuthority() (RuntimeRootAuthority, error) {
	return RuntimeRootAuthority{}, ErrInstalledRuntimeRootUnavailable
}

func ResolveRootAuthority(databaseFolder string) (RuntimeRootAuthority, error) {
	if os.Getenv("SUI_DEPLOYMENT_KIND") == "docker" {
		return RuntimeRootAuthority{}, errors.New("docker server protection runtime root proof is supported only on Linux")
	}
	return resolveRootAuthority(databaseFolder, LoadInstalledRuntimeRoot, func() (deploymentidentity.InstalledApplicationOwnerProjection, error) {
		return deploymentidentity.InstalledApplicationOwnerProjection{}, deploymentidentity.ErrExpectedApplicationOwnerUnavailable
	})
}

func ResolveRoot(databaseFolder string) (string, error) {
	authority, err := ResolveRootAuthority(databaseFolder)
	return authority.Path(), err
}

func recheckRuntimeRootAuthority(RuntimeRootAuthority) error {
	return errors.New("installed Server Protection deployment projection is supported only on Linux")
}
