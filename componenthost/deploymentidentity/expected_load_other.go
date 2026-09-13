//go:build !linux

package deploymentidentity

import "errors"

var ErrExpectedApplicationOwnerUnavailable = errors.New("expected application owner is unavailable")

func LoadExpectedApplicationOwner() (ExpectedApplicationOwnerV1, error) {
	return ExpectedApplicationOwnerV1{}, ErrExpectedApplicationOwnerUnavailable
}

func LoadInstalledApplicationOwnerProjection() (InstalledApplicationOwnerProjection, error) {
	return InstalledApplicationOwnerProjection{}, ErrExpectedApplicationOwnerUnavailable
}

func (p InstalledApplicationOwnerProjection) Recheck() error {
	if err := p.Validate(); err != nil {
		return err
	}
	return ErrExpectedApplicationOwnerUnavailable
}

func LoadExpectedProcdApplicationOwner() (ExpectedApplicationOwnerV1, error) {
	return ExpectedApplicationOwnerV1{}, ErrExpectedApplicationOwnerUnavailable
}

func LoadBoundProcdInstalled() (ApplicationOwnerContractProcdV1, error) {
	return ApplicationOwnerContractProcdV1{}, ErrExpectedApplicationOwnerUnavailable
}
