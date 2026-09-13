//go:build !linux

package update

// Native self-managed update mutation is Linux-only. Portable tests inject the
// same directory-commit boundary; unsupported hosts retain a closed no-op.
func syncUpdateBackupDirectory(string) error { return nil }
