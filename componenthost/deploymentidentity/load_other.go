//go:build !linux

package deploymentidentity

import "errors"

func LoadInstalled() (ApplicationOwnerContractV1, error) {
	return ApplicationOwnerContractV1{}, errors.New("application owner contract is supported only on Linux")
}

func LoadFromPath(string) (ApplicationOwnerContractV1, error) {
	return ApplicationOwnerContractV1{}, errors.New("application owner contract is supported only on Linux")
}

func LoadProcdInstalled() (ApplicationOwnerContractProcdV1, error) {
	return ApplicationOwnerContractProcdV1{}, errors.New("procd application owner contract is supported only on Linux")
}

func LoadProcdFromPath(string) (ApplicationOwnerContractProcdV1, error) {
	return ApplicationOwnerContractProcdV1{}, errors.New("procd application owner contract is supported only on Linux")
}
