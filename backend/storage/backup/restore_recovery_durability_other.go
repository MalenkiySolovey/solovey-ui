//go:build !linux

package backup

// SyncRecoveryDirectory is a no-op where directory fsync is unavailable.
func SyncRecoveryDirectory(string) error { return nil }
