//go:build !linux

package deploymentidentity

import "errors"

func LoadInstalled() (ApplicationOwnerContractV1, error) {
	return ApplicationOwnerContractV1{}, errors.New("application owner contract is supported only on Linux")
}

func LoadFromPath(string) (ApplicationOwnerContractV1, error) {
	return ApplicationOwnerContractV1{}, errors.New("application owner contract is supported only on Linux")
}
