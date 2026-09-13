//go:build !linux

package deploymentadapter

func LoadInstalledRecoveryProjection() (RecoveryProjection, error) {
	return RecoveryProjection{}, ErrInstalledRecoveryProjectionUnavailable
}
