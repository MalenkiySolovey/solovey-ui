//go:build linux

package deploymentadapter

import (
	protectionruntime "github.com/MalenkiySolovey/solovey-ui/components/server-protection/runtimecontract"
)

// LoadInstalledRecoveryProjection selects exactly one authenticated installed
// recovery authority. Backend-specific representation remains in this
// owner-local adapter rather than generic recovery policy.
func LoadInstalledRecoveryProjection() (RecoveryProjection, error) {
	authority, err := protectionruntime.ResolveRootAuthority("/")
	if err != nil {
		return RecoveryProjection{}, err
	}
	return RecoveryProjectionForRuntimeAuthority(authority)
}
