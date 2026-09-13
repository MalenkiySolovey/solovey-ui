//go:build linux

package runtimecontract

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/mountevidence"
)

func ObserveRuntimeMount(root, policy string) (RuntimeMountProofV1, error) {
	if err := validateRuntimeRootBoundary(root); err != nil {
		return RuntimeMountProofV1{}, err
	}
	fact, err := mountevidence.Observe(root)
	if err != nil {
		return RuntimeMountProofV1{}, err
	}
	return NewRuntimeMountProof(root, policy, fact)
}

func recheckRuntimeMount(proof RuntimeMountProofV1) error {
	return recheckRuntimeMountWithObserver(proof, mountevidence.Observe)
}

func recheckRuntimeMountWithObserver(proof RuntimeMountProofV1, observe func(string) (mountevidence.Fact, error)) error {
	if proof.Validate() != nil {
		return errors.New("Server Protection runtime mount proof is invalid")
	}
	if observe == nil {
		return errors.New("Server Protection runtime mount observer is unavailable")
	}
	if err := validateRuntimeRootBoundary(proof.Root); err != nil {
		return errors.Join(errors.New("Server Protection runtime root boundary changed"), err)
	}
	current, err := observe(proof.Root)
	if err != nil || current.Revision != proof.Mount.Revision {
		return errors.Join(errors.New("Server Protection runtime mount identity changed"), err)
	}
	return nil
}

func validateRuntimeRootBoundary(root string) error {
	for _, candidate := range []string{filepath.Clean(root), filepath.Dir(filepath.Clean(root))} {
		info, err := os.Lstat(candidate)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.Join(errors.New("Server Protection runtime root owner boundary is unsafe"), err)
		}
	}
	return nil
}
