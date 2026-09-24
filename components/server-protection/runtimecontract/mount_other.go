//go:build !linux

package runtimecontract

import "errors"

func ObserveRuntimeMount(string, string) (RuntimeMountProofV1, error) {
	return RuntimeMountProofV1{}, errors.New("server protection runtime mount proof is supported only on Linux")
}
