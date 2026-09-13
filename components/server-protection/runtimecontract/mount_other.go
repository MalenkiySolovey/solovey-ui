//go:build !linux

package runtimecontract

import "errors"

func ObserveRuntimeMount(string, string) (RuntimeMountProofV1, error) {
	return RuntimeMountProofV1{}, errors.New("Server Protection runtime mount proof is supported only on Linux")
}

func recheckRuntimeMount(RuntimeMountProofV1) error {
	return errors.New("Server Protection runtime mount proof is supported only on Linux")
}
