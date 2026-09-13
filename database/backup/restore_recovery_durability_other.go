//go:build !linux

package backup

// Non-Linux platforms do not expose the POSIX directory-fsync primitive.
func syncRestoreRecoveryDirectory(string) error { return nil }
